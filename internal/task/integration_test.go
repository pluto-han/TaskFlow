package task

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
	"github.com/pluto-han/taskflow/internal/testdb"

	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/user"
)

func TestTaskOwnershipIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_TASK_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TASKFLOW_TASK_TEST_DATABASE_URL to an isolated taskflow_lesson14_test database migrated to version 9")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson14_test" {
		t.Fatal("requires isolated loopback taskflow_lesson14_test")
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
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 9 || dirty {
		t.Fatal("unexpected migration state")
	}
	users := user.NewPostgresRepository(pool)
	ss, err := session.NewService(ctx, users, session.NewPostgresRepository(pool), auth.PasswordHasher{})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(pool)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	session.Mount(router, ss)
	router.POST("/auth/register", user.NewHandler(user.NewService(users, auth.PasswordHasher{})).Register)
	Mount(router, NewService(repo, nil), session.NewHandler(ss).RequireSession)
	var ids []session.UserID
	var tokens []string
	var tasks []Task
	for i := 0; i < 2; i++ {
		body := fmt.Sprintf(`{"email":"task-%d-%d@example.com","password":"task ownership test passphrase"}`, time.Now().UnixNano(), i)
		rec := taskRequest(router, "POST", "/auth/register", body, "")
		var account user.UserResponse
		if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &account) != nil {
			t.Fatalf("register: %s", rec.Body.String())
		}
		ids = append(ids, session.UserID(account.ID))
		rec = taskRequest(router, "POST", "/auth/login", body, "")
		var login session.LoginResponse
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &login) != nil {
			t.Fatalf("login: %s", rec.Body.String())
		}
		tokens = append(tokens, login.AccessToken)
		rec = taskRequest(router, "POST", "/tasks", `{"title":"Private task","priority":2}`, login.AccessToken)
		var task Task
		if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &task) != nil {
			t.Fatalf("create: %s", rec.Body.String())
		}
		tasks = append(tasks, task)
		var owner string
		if err := pool.QueryRow(ctx, "SELECT owner_id::text FROM tasks WHERE id=$1", task.ID).Scan(&owner); err != nil || owner != account.ID {
			t.Fatal("DB owner differs from authenticated subject")
		}
	}
	for i := 0; i < 2; i++ {
		other := 1 - i
		rec := taskRequest(router, "GET", "/tasks?owner_id="+string(ids[other]), "", tokens[i])
		var list []Task
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].ID != tasks[i].ID {
			t.Fatalf("private list: %d %s", rec.Code, rec.Body.String())
		}
		for _, method := range []string{"GET", "PATCH", "DELETE"} {
			body := `{"title":"Attack","version":1}`
			foreign := taskRequest(router, method, "/tasks/"+tasks[other].ID, body, tokens[i])
			missing := taskRequest(router, method, "/tasks/absent", body, tokens[i])
			if foreign.Code != 404 || foreign.Body.String() != missing.Body.String() {
				t.Fatalf("%s cross-user: %d %s", method, foreign.Code, foreign.Body.String())
			}
		}
		for _, key := range []string{"owner_id", "user_id"} {
			rec := taskRequest(router, "POST", "/tasks", fmt.Sprintf(`{"title":"Attack","priority":2,%q:%q}`, key, ids[other]), tokens[i])
			if rec.Code != 400 {
				t.Fatal("spoofed create accepted")
			}
			rec = taskRequest(router, "PATCH", "/tasks/"+tasks[i].ID, fmt.Sprintf(`{"title":"Attack","version":1,%q:%q}`, key, ids[other]), tokens[i])
			if rec.Code != 400 {
				t.Fatal("ownership transfer accepted")
			}
		}
	}
	// Both requests read version 1 before either can write: exercise SQL CAS.
	synced := &synchronizedReadRepository{Repository: repo}
	synced.readers.Add(2)
	svc := NewService(synced, nil)
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"First", "Second"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			_, err := svc.Update(ctx, ids[0], tasks[0].ID, UpdateTaskRequest{Title: &title, Version: versionPointer(1)})
			outcomes <- err
		}(title)
	}
	wg.Wait()
	close(outcomes)
	successes, conflicts := 0, 0
	for err := range outcomes {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrTaskConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("SQL concurrency: %d success %d conflict", successes, conflicts)
	}
	rec := taskRequest(router, "PATCH", "/tasks/"+tasks[0].ID, `{"title":"Stale","version":1}`, tokens[0])
	if rec.Code != 409 {
		t.Fatal("stale HTTP update not 409")
	}
	saved, err := repo.FindByID(ctx, ids[1], tasks[1].ID)
	if err != nil || saved.Version != 1 || saved.Title != "Private task" {
		t.Fatal("foreign task modified")
	}
	// Direct repository tests ensure isolation is enforced below the HTTP layer.
	repositoryIsolation(t, repo, ids[0], ids[1])
	for _, tc := range []struct{ method, path, body string }{{"GET", "/tasks", ""}, {"GET", "/tasks/" + tasks[0].ID, ""}, {"POST", "/tasks", `{"title":"No auth","priority":2}`}, {"PATCH", "/tasks/" + tasks[0].ID, `{"title":"No auth","version":2}`}, {"DELETE", "/tasks/" + tasks[0].ID, ""}} {
		if rec := taskRequest(router, tc.method, tc.path, tc.body, ""); rec.Code != 401 {
			t.Fatalf("anonymous %s: %d", tc.method, rec.Code)
		}
	}
	if rec := taskRequest(router, "DELETE", "/tasks/"+tasks[0].ID, "", tokens[0]); rec.Code != 204 {
		t.Fatal("own delete failed")
	}
	if rec := taskRequest(router, "GET", "/tasks/"+tasks[0].ID, "", tokens[0]); rec.Code != 404 {
		t.Fatal("deleted task remains")
	}
	if rec := taskRequest(router, "POST", "/auth/logout", "", tokens[0]); rec.Code != 204 {
		t.Fatal("logout failed")
	}
	if rec := taskRequest(router, "GET", "/tasks", "", tokens[0]); rec.Code != 401 {
		t.Fatal("revoked session accessed tasks")
	}
	hash := sha256.Sum256([]byte(tokens[1]))
	if _, err := pool.Exec(ctx, "UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now() WHERE token_hash=$1", hash[:]); err != nil {
		t.Fatal(err)
	}
	if rec := taskRequest(router, "GET", "/tasks", "", tokens[1]); rec.Code != 401 {
		t.Fatal("expired session accessed tasks")
	}
	// Database enforces non-null and foreign-key ownership even outside the API.
	for _, query := range []string{
		"INSERT INTO tasks(id,title,priority) VALUES ('no-owner','invalid',1)",
		"INSERT INTO tasks(id,title,priority,owner_id) VALUES ('bad-owner','invalid',1,'ffffffff-ffff-4fff-8fff-ffffffffffff')",
	} {
		if _, err := pool.Exec(ctx, query); err == nil {
			t.Fatal("invalid ownership accepted")
		}
	}
	// Deleting an owner must not silently cascade-delete their tasks.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id=$1", string(ids[1])); err == nil {
		t.Fatal("owner deleted with tasks")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := repo.FindPage(canceled, ids[0], PageQuery{Limit: 100}); !errors.Is(err, context.Canceled) {
		t.Fatal("context lost")
	}
	// Exercise a task repository outage with valid authentication supplied by fixture.
	pool.Close()
	offline := gin.New()
	fakeSession, _ := session.NewService(ctx, nil, testSessionRepository{}, testPasswords{})
	Mount(offline, NewService(repo, nil), session.NewHandler(fakeSession).RequireSession)
	rec = taskRequest(offline, "GET", "/tasks", "", testToken)
	if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"error":{"code":"INTERNAL_ERROR","message":"Internal server error"}}` {
		t.Fatal("repository error leaked")
	}
}
