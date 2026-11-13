package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	infraaccount "github.com/pluto-han/taskflow/internal/infra/persistence/account"
	interfaceshttpaccount "github.com/pluto-han/taskflow/internal/interfaces/http/account"
	"github.com/pluto-han/taskflow/internal/testdb"
)

// Run via python3 scripts/test_integration.py. Ordinary unit tests never connect to a DB.
func TestPostgresRegistrationIntegration(t *testing.T) {
	ctx, pool := accountDatabase(t)
	repo := infraaccount.New(pool)
	hasher := infrapassword.PasswordHasher{}
	router := registrationRouter(repo, hasher)

	email := fmt.Sprintf("concurrent-%d@example.com", time.Now().UnixNano())
	password := "a long integration passphrase"

	type result struct {
		status int
		body   string
	}

	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, address := range []string{email, "  " + strings.ToUpper(email) + "  "} {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			<-start
			raw, _ := json.Marshal(interfaceshttpaccount.RegisterRequest{
				Email:    address,
				Password: password,
			})
			req := httptest.NewRequest(
				http.MethodPost,
				"/auth/register",
				strings.NewReader(string(raw)),
			).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			results <- result{rec.Code, rec.Body.String()}
		}(address)
	}

	close(start)
	wg.Wait()
	close(results)

	successes, conflicts := 0, 0
	var created interfaceshttpaccount.UserResponse
	for result := range results {
		switch result.status {
		case 201:
			successes++
			if err := json.Unmarshal([]byte(result.body), &created); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.body, "password") || strings.Contains(result.body, "argon2") {
				t.Fatal("response leaks password material")
			}
		case 409:
			conflicts++
			if !strings.Contains(result.body, "EMAIL_ALREADY_EXISTS") {
				t.Fatalf("wrong conflict: %s", result.body)
			}
		default:
			t.Fatalf("unexpected response: %d %s", result.status, result.body)
		}
	}

	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}

	stored, err := repo.GetByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != created.ID ||
		stored.Email != created.Email ||
		stored.CreatedAt.IsZero() ||
		!stored.CreatedAt.Equal(created.CreatedAt) {
		t.Fatal("UUID/email/time mapping mismatch")
	}
	if stored.PasswordHash == password || !strings.HasPrefix(stored.PasswordHash, "$argon2id$") {
		t.Fatal("database must store an Argon2id hash")
	}
	if ok, err := hasher.VerifyPassword(ctx, password, stored.PasswordHash); err != nil || !ok {
		t.Fatalf("persisted hash verification: %v %v", ok, err)
	}

	byID, err := repo.GetByID(ctx, created.ID)
	if err != nil || *byID != *stored {
		t.Fatalf("GetByID: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE email=$1", email).Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("rows=%d err=%v", count, err)
	}

	if _, err := repo.GetByEmail(ctx, "missing-"+email); !errors.Is(err, domainaccount.ErrUserNotFound) {
		t.Fatal(err)
	}
	if _, err := repo.GetByID(ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, domainaccount.ErrUserNotFound) {
		t.Fatal(err)
	}
	if _, err := repo.GetByID(ctx, "invalid"); !errors.Is(err, domainaccount.ErrUserNotFound) {
		t.Fatal(err)
	}

	// Exercise the database constraint directly, bypassing service normalization.
	_, err = pool.Exec(
		ctx,
		"INSERT INTO users(id,email,password_hash) VALUES($1,$2,$3)",
		"00000000-0000-4000-8000-000000000001",
		" Upper@Example.com ",
		"test-only",
	)
	if err == nil {
		t.Fatal("database accepted non-normalized email")
	}

	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := repo.GetByEmail(canceled, email); !errors.Is(err, context.Canceled) {
		t.Fatalf("context not propagated: %v", err)
	}

	// Closed pool simulates a real database failure; the HTTP body must stay generic.
	pool.Close()
	req := httptest.NewRequest(
		http.MethodPost,
		"/auth/register",
		strings.NewReader(`{"email":"failure@example.com","password":"a long passphrase"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 500 ||
		strings.TrimSpace(rec.Body.String()) != `{"error":{"code":"INTERNAL_ERROR","message":"Internal server error"}}` {
		t.Fatalf("database error response: %d %s", rec.Code, rec.Body.String())
	}
}

func registrationRouter(
	repo domainaccount.Repository,
	passwords applicationaccount.PasswordHasher,
) *gin.Engine {
	router := gin.New()
	router.POST("/auth/register", interfaceshttpaccount.New(applicationaccount.New(repo, passwords)).Register)

	return router
}

func accountDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

	url := os.Getenv("TASKFLOW_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run python3 scripts/test_integration.py for isolated PostgreSQL tests")
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	if cfg.ConnConfig.Database != "taskflow_lesson12_test" || cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("integration tests require loopback and taskflow_lesson12_test database")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil ||
		version != 17 ||
		dirty {
		t.Fatalf("schema: %d %v %v", version, dirty, err)
	}

	return ctx, pool
}
