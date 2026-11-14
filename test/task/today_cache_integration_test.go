package task_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

func TestTodayCachePostgresReadContract(t *testing.T) {
	raw := os.Getenv("TASKFLOW_API_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run make test-integration for a guarded disposable PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson17_test" {
		t.Fatal("isolated API database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot open fixture database")
	}
	defer pool.Close()
	owner := domainaccount.UserID("00000000-0000-4000-8000-000000000039")
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES ($1,'today-cache39@example.com','fixture-only')", string(owner)); err != nil {
		t.Fatal("cannot create cache fixture user")
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tasks WHERE owner_id=$1", string(owner))
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", string(owner))
	}()
	repo := &todayCountingRepository{Repository: infratask.New(pool)}
	store := &todayStore{values: map[string][]byte{}}
	svc := applicationtask.NewWithClock(repo, nil, todayTime)
	if _, err := svc.CreateToday(ctx, owner, applicationtask.CreateTodayInput{PlanDate: "2026-11-05", Title: "PostgreSQL Today fixture", Priority: 2}); err != nil {
		t.Fatal(err)
	}
	before := readCachedToday(t, svc, owner)
	svc.WithTodayCache(store)
	miss := readCachedToday(t, svc, owner)
	hit := readCachedToday(t, svc, owner)
	if repo.reads != 2 || store.gets != 2 || store.sets != 1 || hit.Items[0].OwnerID != owner {
		t.Fatal("cache hit repeated the real PostgreSQL FindPage query")
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(miss)
	c, _ := json.Marshal(hit)
	if string(a) != string(b) || string(b) != string(c) {
		t.Fatal("cache changed the PostgreSQL Today response")
	}
}
