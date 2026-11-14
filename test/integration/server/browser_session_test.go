package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	infradatabase "github.com/pluto-han/taskflow/internal/infra/database"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	infraaccount "github.com/pluto-han/taskflow/internal/infra/persistence/account"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
	infrasession "github.com/pluto-han/taskflow/internal/infra/persistence/session"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	interfaceshttpsession "github.com/pluto-han/taskflow/internal/interfaces/http/session"
	interfaceshttptask "github.com/pluto-han/taskflow/internal/interfaces/http/task"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

// Uses only the disposable, leased database created by test_integration.py.
// The API pool SET ROLEs to a non-owner with the production session/user grants.
func TestBrowserSessionHTTPIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_BROWSER_SESSION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run python3 scripts/test_integration.py for isolated browser-session tests")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson45_test" {
		t.Fatal("requires the isolated loopback lesson45 database")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot connect isolated database")
	}
	defer admin.Close()
	var version int
	var dirty bool
	if err := admin.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatal("requires clean migration 15")
	}
	// The runner created this row before v14. It must still authenticate as Bearer.
	legacyHash := bytes.Repeat([]byte{0xab}, 32)
	var oldHash [32]byte
	copy(oldHash[:], legacyHash)
	if _, err := infrasession.New(admin).FindActive(ctx, oldHash); err != nil {
		t.Fatal("upgrade invalidated the v13 session")
	}

	role := "browser_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{role}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS")
	if err != nil {
		t.Fatal("cannot create fixture role")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP OWNED BY "+quoted+"; DROP ROLE "+quoted); err != nil {
			t.Error("fixture role cleanup failed")
		}
	}()
	_, err = admin.Exec(ctx, "REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO "+quoted+
		"; GRANT SELECT,INSERT ON users TO "+quoted+"; GRANT UPDATE(password_hash,credentials_version) ON users TO "+quoted+"; GRANT SELECT,INSERT,UPDATE ON sessions TO "+quoted+
		"; GRANT SELECT,INSERT,UPDATE,DELETE ON tasks,projects,project_memberships,project_invitations TO "+quoted)
	if err != nil {
		t.Fatal("fixture grants failed")
	}
	runtimeCfg := cfg.Copy()
	runtimeCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+quoted)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, runtimeCfg)
	if err != nil {
		t.Fatal("cannot open runtime pool")
	}
	defer pool.Close()
	if err := infradatabase.CheckRuntimeRole(ctx, pool); err != nil {
		t.Fatal("runtime role is privileged")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET email='forbidden' WHERE false"); err == nil {
		t.Fatal("runtime unexpectedly has identity UPDATE")
	}
	users := infraaccount.New(pool)
	repo := infrasession.New(pool)
	sessions, err := applicationsession.New(ctx, users, repo, infrapassword.PasswordHasher{})
	if err != nil {
		t.Fatal("session setup failed")
	}
	projects := infraproject.New(pool)
	var logs lockedAuthLog
	router := interfaceshttprouter.New(interfaceshttprouter.Dependencies{
		Users: applicationaccount.New(users, infrapassword.PasswordHasher{}), Sessions: sessions,
		Tasks: applicationtask.New(infratask.New(pool), projects), Projects: applicationproject.New(projects, projects),
		Security: interfaceshttpmiddleware.SecurityOptions{PublicOrigin: "http://127.0.0.1:5173"},
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)), Ping: pool.Ping,
	})
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	plain := &http.Client{Timeout: 5 * time.Second}
	perform := func(client *http.Client, method, path, body, csrf, authorization string, cookie *http.Cookie, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, httpServer.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal("request setup failed")
		}
		req.Header.Set("Content-Type", "application/json")
		if method != "GET" {
			req.Header.Set("Origin", "http://127.0.0.1:5173")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("isolated HTTP request failed")
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s status=%d want=%d", method, path, response.StatusCode, want)
		}
		return raw
	}
	password := "browser integration passphrase"
	loginBody := `{"email":"browser45@example.com","password":"` + password + `"}`
	perform(plain, "POST", "/auth/register", loginBody, "", "", nil, 201)
	var logged interfaceshttpsession.BrowserResponse
	raw := perform(client, "POST", "/auth/browser/login", loginBody, "", "", nil, 200)
	if json.Unmarshal(raw, &logged) != nil || logged.User.ID == "" || logged.CSRFToken == "" {
		t.Fatal("invalid login DTO")
	}
	endpoint, _ := http.NewRequest("GET", httpServer.URL, nil)
	cookies := jar.Cookies(endpoint.URL)
	if len(cookies) != 1 {
		t.Fatal("session cookie not retained")
	}
	cookie := cookies[0]
	digest := sha256.Sum256([]byte(cookie.Value))
	var storedHash, csrf []byte
	var kind string
	var expiry time.Time
	if err := admin.QueryRow(ctx, "SELECT token_hash,csrf_secret,credential_kind,expires_at FROM sessions WHERE token_hash=$1", digest[:]).Scan(&storedHash, &csrf, &kind, &expiry); err != nil || kind != "browser" || !bytes.Equal(storedHash, digest[:]) || len(csrf) != 32 || !expiry.Equal(logged.ExpiresAt) {
		t.Fatal("browser storage mismatch")
	}
	if bytes.Contains(raw, []byte(cookie.Value)) || bytes.Contains(raw, []byte("access_token")) {
		t.Fatal("credential in JSON")
	}
	newPage := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	for i := 0; i < 2; i++ {
		var restored interfaceshttpsession.BrowserResponse
		raw := perform(newPage, "GET", "/auth/browser/session", "", "", "", nil, 200)
		if json.Unmarshal(raw, &restored) != nil || restored.CSRFToken != logged.CSRFToken || !restored.ExpiresAt.Equal(expiry) || restored.User.ID != logged.User.ID {
			t.Fatal("bootstrap changed identity, expiry or CSRF")
		}
	}
	// Credential kinds are enforced in the real SQL, not only in an HTTP adapter.
	perform(plain, "GET", "/auth/me", "", "", "Bearer "+cookie.Value, nil, 401)
	var bearer interfaceshttpsession.LoginResponse
	if json.Unmarshal(perform(plain, "POST", "/auth/login", loginBody, "", "", nil, 200), &bearer) != nil {
		t.Fatal("legacy login failed")
	}
	perform(plain, "GET", "/auth/me", "", "", "Bearer "+bearer.AccessToken, nil, 200)
	perform(plain, "GET", "/auth/browser/session", "", "", "", &http.Cookie{Name: cookie.Name, Value: bearer.AccessToken}, 401)
	perform(client, "GET", "/tasks", "", "", "Bearer "+bearer.AccessToken, nil, 400)
	perform(client, "POST", "/tasks", `{"title":"must not be written","priority":2}`, "", "", nil, 403)
	var count int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE owner_id=$1", logged.User.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("CSRF rejection wrote a task")
	}
	taskRaw := perform(client, "POST", "/tasks", `{"title":"cookie task","priority":2}`, logged.CSRFToken, "", nil, 201)
	var created interfaceshttptask.TaskResponse
	if json.Unmarshal(taskRaw, &created) != nil || created.ID == "" {
		t.Fatal("cookie task write failed")
	}
	// A second account can authenticate but cannot read this task.
	otherJar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: otherJar, Timeout: 5 * time.Second}
	otherBody := `{"email":"other45@example.com","password":"` + password + `"}`
	perform(plain, "POST", "/auth/register", otherBody, "", "", nil, 201)
	perform(other, "POST", "/auth/browser/login", otherBody, "", "", nil, 200)
	perform(other, "GET", "/tasks/"+created.ID, "", "", "", nil, 404)
	// Kind/secret invariants are database constraints available to the runtime role.
	for _, query := range []string{
		"UPDATE sessions SET csrf_secret=NULL WHERE token_hash=$1",
		"UPDATE sessions SET csrf_secret=decode('00','hex') WHERE token_hash=$1",
		"UPDATE sessions SET credential_kind='bearer' WHERE token_hash=$1",
		"UPDATE sessions SET credential_kind='invalid' WHERE token_hash=$1",
	} {
		_, err := pool.Exec(ctx, query, digest[:])
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatal("credential constraint not enforced")
		}
	}
	perform(client, "POST", "/auth/browser/logout", "", logged.CSRFToken, "", nil, 204)
	if len(jar.Cookies(endpoint.URL)) != 0 {
		t.Fatal("logout cookie still retained")
	}
	perform(plain, "GET", "/auth/browser/session", "", "", "", cookie, 401)
	perform(plain, "GET", "/auth/me", "", "", "Bearer "+bearer.AccessToken, nil, 200)
	perform(other, "GET", "/auth/browser/session", "", "", "", nil, 200)
	// Expiry equality must be rejected by SQL. Only the disposable fixture is edited.
	perform(client, "POST", "/auth/browser/login", loginBody, "", "", nil, 200)
	newCookies := jar.Cookies(endpoint.URL)
	expiredHash := sha256.Sum256([]byte(newCookies[0].Value))
	if _, err := admin.Exec(ctx, "UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now() WHERE token_hash=$1", expiredHash[:]); err != nil {
		t.Fatal("expiry fixture failed")
	}
	perform(client, "GET", "/auth/browser/session", "", "", "", nil, 401)
	for _, secret := range []string{password, cookie.Value, bearer.AccessToken, logged.CSRFToken} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret in HTTP logs")
		}
	}
}

type lockedAuthLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedAuthLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *lockedAuthLog) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
