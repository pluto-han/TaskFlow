package server_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	"github.com/pluto-han/taskflow/internal/config"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	"github.com/pluto-han/taskflow/internal/httpapi"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	infraaccount "github.com/pluto-han/taskflow/internal/infra/persistence/account"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/testdb"
)

type accountFixture struct {
	ctx         context.Context
	admin, pool *pgxpool.Pool
	users       *infraaccount.Repository
	repo        *session.PostgresRepository
	svc         *session.Service
}

func accountDatabase(t *testing.T) *accountFixture {
	t.Helper()
	url := os.Getenv("TASKFLOW_ACCOUNT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run scripts/test_integration.py for isolated account tests")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson47_test" {
		t.Fatal("requires isolated loopback lesson47 database")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("fixture connection failed")
	}
	t.Cleanup(admin.Close)
	var version int
	var dirty bool
	if err := admin.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatal("requires clean schema 17")
	}
	role := "account_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS"); err != nil {
		t.Fatal("fixture role failed")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP OWNED BY "+quoted+"; DROP ROLE "+quoted); err != nil {
			t.Error("role cleanup failed")
		}
	})
	if _, err := admin.Exec(ctx, "REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO "+quoted+
		"; GRANT SELECT,INSERT ON users TO "+quoted+"; GRANT UPDATE(password_hash,credentials_version,email_verified_at) ON users TO "+quoted+
		"; GRANT SELECT,INSERT,UPDATE,DELETE ON email_tokens,email_outbox,email_reset_requests TO "+quoted+"; GRANT SELECT,INSERT,UPDATE ON sessions TO "+quoted); err != nil {
		t.Fatal("fixture grants failed")
	}
	runtimeCfg := cfg.Copy()
	runtimeCfg.ConnConfig.RuntimeParams["role"] = role
	pool, err := pgxpool.NewWithConfig(ctx, runtimeCfg)
	if err != nil {
		t.Fatal("runtime connection failed")
	}
	t.Cleanup(pool.Close)
	if err := config.CheckRuntimeRole(ctx, pool); err != nil {
		t.Fatal("runtime is privileged")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET email='forbidden' WHERE false"); err == nil {
		t.Fatal("runtime can change account identity")
	}
	users := infraaccount.New(pool)
	repo := session.NewPostgresRepository(pool)
	svc, err := session.NewService(ctx, users, repo, infrapassword.PasswordHasher{})
	if err != nil {
		t.Fatal("session setup failed")
	}
	return &accountFixture{ctx: ctx, admin: admin, pool: pool, users: users, repo: repo, svc: svc}
}

const accountPassword = "original account passphrase"

func (f *accountFixture) account(t *testing.T) (*domainaccount.User, session.LoginResponse, session.Principal) {
	t.Helper()
	u, err := applicationaccount.New(f.users, infrapassword.PasswordHasher{}).Register(f.ctx, applicationaccount.CreateInput{Email: strings.ToLower(rand.Text()) + "@example.com", Password: accountPassword})
	if err != nil {
		t.Fatal("account creation failed")
	}
	login, err := f.svc.Login(f.ctx, session.LoginRequest{Email: u.Email, Password: accountPassword})
	if err != nil {
		t.Fatal("account login failed")
	}
	p, err := f.svc.Authenticate(f.ctx, login.AccessToken)
	if err != nil {
		t.Fatal("account authentication failed")
	}
	stored, err := f.users.GetByID(f.ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stored, login, p
}

func TestAccountManagementHTTPIntegration(t *testing.T) {
	f := accountDatabase(t)
	u, first, p := f.account(t)
	other, otherLogin, otherP := f.account(t)
	router := server.NewRouter(server.Dependencies{Sessions: f.svc, Security: httpapi.SecurityOptions{PublicOrigin: "http://127.0.0.1:5173"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	request := func(method, path, body, bearer string, cookie *http.Cookie, csrf string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if cookie != nil {
			req.AddCookie(cookie)
			if method != "GET" {
				req.Header.Set("Origin", "http://127.0.0.1:5173")
			}
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: status=%d want=%d", method, path, rec.Code, status)
		}
		return rec
	}
	input, _ := json.Marshal(session.LoginRequest{Email: u.Email, Password: accountPassword})
	request("POST", "/auth/browser/login", string(input), "", nil, "", 403)
	// // Login requires Origin even without a preexisting cookie.
	browser, token, err := f.svc.LoginBrowser(f.ctx, session.LoginRequest{Email: u.Email, Password: accountPassword})
	if err != nil {
		t.Fatal("browser fixture login failed")
	}
	cookie := &http.Cookie{Name: "taskflow_session_dev", Value: token}
	// Equal creation times exercise the UUID tie-breaker rather than time alone.
	if _, err := f.admin.Exec(f.ctx, "UPDATE sessions SET created_at=now()-interval '1 minute' WHERE user_id=$1", u.ID); err != nil {
		t.Fatal("pagination fixture failed")
	}
	var page session.SessionPage
	if json.Unmarshal(request("GET", "/auth/sessions?limit=1", "", first.AccessToken, nil, "", 200).Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("invalid first session page")
	}
	seen := map[string]bool{page.Items[0].ID: true}
	current := page.Items[0].Current
	if json.Unmarshal(request("GET", "/auth/sessions?limit=1&cursor="+page.NextCursor, "", first.AccessToken, nil, "", 200).Body.Bytes(), &page) != nil || len(page.Items) != 1 || seen[page.Items[0].ID] || page.NextCursor != "" {
		t.Fatal("keyset pagination lost/duplicated a session")
	}
	current = current || page.Items[0].Current
	if !current {
		t.Fatal("requesting session not marked current")
	}
	_, err = f.svc.ListSessions(f.ctx, otherP, "cursor="+base64Cursor(t, p.UserID, page.Items[0]))
	if !errors.Is(err, session.ErrInvalidSessionQuery) {
		t.Fatal("foreign cursor accepted")
	}
	request("DELETE", "/auth/sessions/"+otherP.SessionID, "", first.AccessToken, nil, "", 404)
	request("DELETE", "/auth/sessions/00000000-0000-4000-8000-000000000099", "", first.AccessToken, nil, "", 404)
	request("GET", "/auth/me", "", otherLogin.AccessToken, nil, "", 200)
	request("POST", "/auth/password/change", `{"current_password":"wrong password","new_password":"new account passphrase"}`, "", cookie, browser.CSRFToken, 400)
	request("GET", "/auth/browser/session", "", "", cookie, "", 200)
	changed := request("POST", "/auth/password/change", `{"current_password":"original account passphrase","new_password":"new account passphrase"}`, "", cookie, browser.CSRFToken, 204)
	if len(changed.Result().Cookies()) != 1 || changed.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("password change did not clear current cookie")
	}
	request("GET", "/auth/me", "", first.AccessToken, nil, "", 401)
	request("GET", "/auth/browser/session", "", "", cookie, "", 401)
	request("GET", "/auth/me", "", otherLogin.AccessToken, nil, "", 200)
	stored, err := f.users.GetByID(f.ctx, u.ID)
	if err != nil || stored.CredentialsVersion != 1 || stored.PasswordHash == u.PasswordHash {
		t.Fatal("password/epoch not updated atomically")
	}
	if _, err := f.svc.Login(f.ctx, session.LoginRequest{Email: u.Email, Password: accountPassword}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal("old password still works")
	}
	fresh, err := f.svc.Login(f.ctx, session.LoginRequest{Email: u.Email, Password: "new account passphrase"})
	if err != nil {
		t.Fatal("new password does not work")
	}
	freshP, err := f.svc.Authenticate(f.ctx, fresh.AccessToken)
	if err != nil {
		t.Fatal("fresh authentication failed")
	}
	request("DELETE", "/auth/sessions/"+freshP.SessionID, "", fresh.AccessToken, nil, "", 204)
	request("GET", "/auth/me", "", fresh.AccessToken, nil, "", 401)
	// Current-cookie self revocation must remove the cookie too.
	newBrowser, newToken, err := f.svc.LoginBrowser(f.ctx, session.LoginRequest{Email: other.Email, Password: accountPassword})
	if err != nil {
		t.Fatal("other browser setup failed")
	}
	newP, err := f.svc.AuthenticateBrowser(f.ctx, newToken)
	if err != nil {
		t.Fatal("other browser authentication failed")
	}
	self := request("DELETE", "/auth/sessions/"+strings.ToUpper(newP.SessionID), "", "", &http.Cookie{Name: cookie.Name, Value: newToken}, newBrowser.CSRFToken, 204)
	if len(self.Result().Cookies()) != 1 || self.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("self revoke did not clear browser cookie")
	}
}

func base64Cursor(t *testing.T, owner session.UserID, item session.SessionInfo) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"owner": owner, "created_at": item.CreatedAt, "id": item.ID})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

type barrierPasswords struct {
	infrapassword.PasswordHasher
	arrived chan struct{}
	release <-chan struct{}
}

func (h barrierPasswords) VerifyPassword(ctx context.Context, password, hash string) (bool, error) {
	ok, err := h.PasswordHasher.VerifyPassword(ctx, password, hash)
	if err != nil || !ok {
		return ok, err
	}
	select {
	case h.arrived <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case <-h.release:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
func waitForPassword(t *testing.T, f *accountFixture, arrived <-chan struct{}) {
	t.Helper()
	select {
	case <-arrived:
	case <-f.ctx.Done():
		t.Fatal("password barrier timed out")
	}
}

func TestPasswordChangeClosesLoginRace(t *testing.T) {
	f := accountDatabase(t)
	for _, kind := range []string{"bearer", "browser"} {
		t.Run(kind, func(t *testing.T) {
			u, _, p := f.account(t)
			arrived := make(chan struct{}, 1)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			blocked, err := session.NewService(f.ctx, f.users, f.repo, barrierPasswords{arrived: arrived, release: release})
			if err != nil {
				t.Fatal("barrier service failed")
			}
			type outcome struct {
				token string
				err   error
			}
			result := make(chan outcome, 1)
			go func() {
				input := session.LoginRequest{Email: u.Email, Password: accountPassword}
				if kind == "browser" {
					_, token, err := blocked.LoginBrowser(f.ctx, input)
					result <- outcome{token, err}
				} else {
					login, err := blocked.Login(f.ctx, input)
					result <- outcome{login.AccessToken, err}
				}
			}()
			waitForPassword(t, f, arrived)
			// The paused hash/verification phase must not hold a user-row lock.
			tx, err := f.admin.Begin(f.ctx)
			if err != nil {
				t.Fatal("lock probe failed")
			}
			_, lockErr := tx.Exec(f.ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", u.ID)
			_ = tx.Rollback(f.ctx)
			if lockErr != nil {
				t.Fatal("password work holds a database row lock")
			}
			if err := f.svc.ChangePassword(f.ctx, p, session.PasswordChangeRequest{CurrentPassword: accountPassword, NewPassword: "changed during login"}); err != nil {
				t.Fatal("concurrent password change failed")
			}
			unblock()
			got := <-result
			if !errors.Is(got.err, session.ErrUnauthorized) || got.token != "" {
				t.Fatal("stale password verification issued a new session")
			}
			var active int
			if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL", u.ID).Scan(&active); err != nil || active != 0 {
				t.Fatal("stale session survived password change")
			}
		})
	}
}

func TestConcurrentPasswordChanges(t *testing.T) {
	f := accountDatabase(t)
	u, _, p := f.account(t)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	blocked, err := session.NewService(f.ctx, f.users, f.repo, barrierPasswords{arrived: arrived, release: release})
	if err != nil {
		t.Fatal("barrier service failed")
	}
	type outcome struct {
		password string
		err      error
	}
	results := make(chan outcome, 2)
	for _, password := range []string{"first competing password", "second competing password"} {
		go func(password string) {
			results <- outcome{password, blocked.ChangePassword(f.ctx, p, session.PasswordChangeRequest{CurrentPassword: accountPassword, NewPassword: password})}
		}(password)
	}
	waitForPassword(t, f, arrived)
	waitForPassword(t, f, arrived)
	unblock()
	success, conflict := 0, 0
	winner := ""
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err == nil {
			success++
			winner = got.password
		} else if errors.Is(got.err, session.ErrCredentialsChanged) {
			conflict++
		} else {
			t.Fatal("unexpected concurrent change failure")
		}
	}
	stored, err := f.users.GetByID(f.ctx, u.ID)
	if err != nil || success != 1 || conflict != 1 || stored.CredentialsVersion != 1 {
		t.Fatal("concurrent changes did not serialize")
	}
	if ok, err := (infrapassword.PasswordHasher{}).VerifyPassword(f.ctx, winner, stored.PasswordHash); err != nil || !ok {
		t.Fatal("winning hash was not stored")
	}
}

func TestPasswordChangeRollbackAndRevokedActor(t *testing.T) {
	f := accountDatabase(t)
	u, login, p := f.account(t)
	_, err := f.admin.Exec(f.ctx, `CREATE FUNCTION reject_session_revoke() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture rejection'; END $$;
CREATE CONSTRAINT TRIGGER reject_session_revoke AFTER UPDATE ON sessions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_session_revoke();`)
	if err != nil {
		t.Fatal("deferred commit-failure fixture failed")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = f.admin.Exec(cleanup, "DROP TRIGGER IF EXISTS reject_session_revoke ON sessions; DROP FUNCTION IF EXISTS reject_session_revoke()")
	})
	if err := f.svc.ChangePassword(f.ctx, p, session.PasswordChangeRequest{CurrentPassword: accountPassword, NewPassword: "must be rolled back"}); err == nil {
		t.Fatal("expected deferred commit failure")
	}
	if _, err := f.admin.Exec(f.ctx, "DROP TRIGGER reject_session_revoke ON sessions; DROP FUNCTION reject_session_revoke()"); err != nil {
		t.Fatal("fault cleanup failed")
	}
	stored, err := f.users.GetByID(f.ctx, u.ID)
	if err != nil || stored.PasswordHash != u.PasswordHash || stored.CredentialsVersion != 0 {
		t.Fatal("failed transaction changed credentials")
	}
	if _, err := f.svc.Authenticate(f.ctx, login.AccessToken); err != nil {
		t.Fatal("failed transaction revoked the session")
	}
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	blocked, err := session.NewService(f.ctx, f.users, f.repo, barrierPasswords{arrived: arrived, release: release})
	if err != nil {
		t.Fatal("barrier service failed")
	}
	result := make(chan error, 1)
	go func() {
		result <- blocked.ChangePassword(f.ctx, p, session.PasswordChangeRequest{CurrentPassword: accountPassword, NewPassword: "revoked actor password"})
	}()
	waitForPassword(t, f, arrived)
	if err := f.svc.Logout(f.ctx, p); err != nil {
		t.Fatal("actor revoke failed")
	}
	unblock()
	if err := <-result; !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal("revoked actor changed the password")
	}
	stored, err = f.users.GetByID(f.ctx, u.ID)
	if err != nil || stored.PasswordHash != u.PasswordHash || stored.CredentialsVersion != 0 {
		t.Fatal("revoked actor mutated credentials")
	}
}
