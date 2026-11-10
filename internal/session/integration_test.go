package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/testdb"
	"github.com/pluto-han/taskflow/internal/user"
)

func TestSessionIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_SESSION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run python3 scripts/test_integration.py for PostgreSQL tests")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson13_test" {
		t.Fatal("requires isolated loopback taskflow_lesson13_test database")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 14 || dirty {
		t.Fatal("unexpected migration state")
	}
	users := user.NewPostgresRepository(pool)
	sessions := NewPostgresRepository(pool)
	service, err := NewService(ctx, users, sessions, auth.PasswordHasher{})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router, service)
	router.POST("/auth/register", user.NewHandler(user.NewService(users, auth.PasswordHasher{})).Register)
	emails := []string{fmt.Sprintf("session-a-%d@example.com", time.Now().UnixNano()), fmt.Sprintf("session-b-%d@example.com", time.Now().UnixNano())}
	password := "session integration passphrase"
	var accounts []user.UserResponse
	for _, email := range emails {
		raw, _ := json.Marshal(user.RegisterRequest{Email: email, Password: password})
		rec := request(router, "POST", "/auth/register", string(raw), "")
		if rec.Code != 201 {
			t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
		}
		var account user.UserResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, account)
	}
	login := func(email string) LoginResponse {
		t.Helper()
		raw, _ := json.Marshal(LoginRequest{Email: " " + strings.ToUpper(email) + " ", Password: password})
		rec := request(router, "POST", "/auth/login", string(raw), "")
		if rec.Code != 200 {
			t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
		}
		var result LoginResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rec.Header().Get("Cache-Control") != "no-store" || result.TokenType != "Bearer" {
			t.Fatal("invalid token response headers/type")
		}
		return result
	}
	a1, a2, b := login(emails[0]), login(emails[0]), login(emails[1])
	if a1.AccessToken == a2.AccessToken || a1.AccessToken == b.AccessToken {
		t.Fatal("tokens reused")
	}
	hash := sha256.Sum256([]byte(a1.AccessToken))
	var storedHash []byte
	var rowJSON string
	var expiry time.Time
	err = pool.QueryRow(ctx, "SELECT token_hash, row_to_json(s)::text, expires_at FROM sessions s WHERE token_hash=$1", hash[:]).Scan(&storedHash, &rowJSON, &expiry)
	if err != nil || string(storedHash) != string(hash[:]) || strings.Contains(rowJSON, a1.AccessToken) || !expiry.Equal(a1.ExpiresAt) {
		t.Fatal("token storage/mapping failed")
	}
	if ttl := time.Until(expiry); ttl < 23*time.Hour || ttl > 24*time.Hour {
		t.Fatalf("unexpected TTL %v", ttl)
	}
	// Wrong password and missing account have identical externally visible failures.
	wrong := request(router, "POST", "/auth/login", fmt.Sprintf(`{"email":%q,"password":"wrong long password"}`, emails[0]), "")
	missing := request(router, "POST", "/auth/login", `{"email":"missing@example.com","password":"wrong long password"}`, "")
	if wrong.Code != 401 || missing.Code != 401 || wrong.Body.String() != missing.Body.String() {
		t.Fatal("credential enumeration via error response")
	}
	for _, tc := range []struct{ token, id string }{{a1.AccessToken, accounts[0].ID}, {a2.AccessToken, accounts[0].ID}, {b.AccessToken, accounts[1].ID}} {
		rec := request(router, "GET", "/auth/me?user_id=forged", "", "Bearer "+tc.token)
		var me user.UserResponse
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &me) != nil || me.ID != tc.id || strings.Contains(rec.Body.String(), "password") {
			t.Fatal("identity isolation failed")
		}
	}
	principal, err := service.Authenticate(ctx, a1.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	forged := principal
	forged.UserID = UserID(accounts[1].ID)
	if err := sessions.Revoke(ctx, forged); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("cross-user revoke accepted")
	}
	// Two concurrent logout requests may both authenticate; exactly one revokes.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			statuses <- request(router, "POST", "/auth/logout", `{"user_id":"forged"}`, "Bearer "+a1.AccessToken).Code
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[204] != 1 || counts[401] != 1 {
		t.Fatalf("logout outcomes: %v", counts)
	}
	if rec := request(router, "GET", "/auth/me", "", "Bearer "+a1.AccessToken); rec.Code != 401 {
		t.Fatal("revoked token still works")
	}
	for _, token := range []string{a2.AccessToken, b.AccessToken} {
		if rec := request(router, "GET", "/auth/me", "", "Bearer "+token); rec.Code != 200 {
			t.Fatal("logout revoked another session")
		}
	}
	// Equality at expiry is invalid, not just strictly older timestamps.
	a2hash := sha256.Sum256([]byte(a2.AccessToken))
	if _, err := pool.Exec(ctx, "UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now() WHERE token_hash=$1", a2hash[:]); err != nil {
		t.Fatal(err)
	}
	if rec := request(router, "GET", "/auth/me", "", "Bearer "+a2.AccessToken); rec.Code != 401 {
		t.Fatal("expired token works")
	}
	if rec := request(router, "POST", "/auth/logout", "", "Bearer "+a2.AccessToken); rec.Code != 401 {
		t.Fatal("expired logout accepted")
	}
	// A user removed from the database cannot retain an active session.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id=$1", accounts[1].ID); err != nil {
		t.Fatal(err)
	}
	if rec := request(router, "GET", "/auth/me", "", "Bearer "+b.AccessToken); rec.Code != 401 {
		t.Fatal("deleted user session works")
	}
	// Enforce hash uniqueness at the database boundary.
	if _, err := sessions.Create(ctx, "00000000-0000-4000-8000-000000000009", UserID(accounts[0].ID), hash); err == nil {
		t.Fatal("duplicate token hash accepted")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := sessions.FindActive(canceled, hash); !errors.Is(err, context.Canceled) {
		t.Fatalf("context: %v", err)
	}
	pool.Close()
	rec := request(router, "GET", "/auth/me", "", "Bearer "+a1.AccessToken)
	if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"error":{"code":"INTERNAL_ERROR","message":"Internal server error"}}` {
		t.Fatal("database outage not generic 500")
	}
}
