package cache_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	infradatabase "github.com/pluto-han/taskflow/internal/infra/database"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
	infrarecurrence "github.com/pluto-han/taskflow/internal/infra/persistence/recurrence"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

type invalidationStore struct {
	values   map[string][]byte
	deletes  int
	onDelete func(context.Context, string) error
}

func (s *invalidationStore) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, applicationtask.ErrMiss
	}
	return append([]byte(nil), value...), nil
}
func (s *invalidationStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.values[key] = append([]byte(nil), value...)
	return nil
}
func (s *invalidationStore) Delete(ctx context.Context, key string) error {
	s.deletes++
	if s.onDelete != nil {
		if err := s.onDelete(ctx, key); err != nil {
			return err
		}
	}
	delete(s.values, key)
	return nil
}

func TestCacheInvalidationPostgresCommitContract(t *testing.T) {
	raw := os.Getenv("TASKFLOW_CACHE_INVALIDATION_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run make test-integration for the isolated invalidation database")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson40_test" {
		t.Fatal("isolated lesson40 database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot open fixture database")
	}
	defer pool.Close()
	workerCfg, err := infradatabase.WorkerPoolConfig(raw)
	if err != nil {
		t.Fatal("cannot configure worker pool")
	}
	workerPool, err := pgxpool.NewWithConfig(ctx, workerCfg)
	if err != nil {
		t.Fatal("cannot open worker pool")
	}
	defer workerPool.Close()
	a := domainaccount.UserID("00000000-0000-4000-8000-000000000040")
	b := domainaccount.UserID("00000000-0000-4000-8000-000000000041")
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES ($1,'cache40a@example.com','fixture'),($2,'cache40b@example.com','fixture')", string(a), string(b)); err != nil {
		t.Fatal("cannot create fixture users")
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	setup := func(t *testing.T) (*applicationtask.Service, *invalidationStore) {
		t.Helper()
		// This dedicated database contains only this test's fixtures and lease.
		for _, table := range []string{"notification_outbox", "tasks", "recurrence_occurrences", "recurrence_rules"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table); err != nil {
				t.Fatal("cannot reset isolated fixture")
			}
		}
		now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
		store := &invalidationStore{values: map[string][]byte{}}
		return applicationtask.NewWithClock(infratask.New(pool), nil, clock).WithTodayCache(store), store
	}
	read := func(t *testing.T, svc *applicationtask.Service, owner domainaccount.UserID) applicationtask.TodayResult {
		t.Helper()
		page, err := svc.ListToday(ctx, owner, applicationtask.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	rules := applicationrecurrence.NewWithClock(infrarecurrence.New(pool), clock)
	createRule := func(t *testing.T, owner domainaccount.UserID) {
		t.Helper()
		if _, err := rules.Create(ctx, owner, applicationrecurrence.CreateInput{Title: "Cache invalidation fixture", Priority: 2, AnchorDate: "2026-11-05", IntervalDays: 1}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("task writes commit before delete", func(t *testing.T) {
		svc, store := setup(t)
		_ = read(t, svc, a)
		wantCount, wantVersion := 1, int64(1)
		store.onDelete = func(cleanup context.Context, _ string) error {
			var count int
			var version int64
			if err := workerPool.QueryRow(cleanup, "SELECT count(*),COALESCE(max(version),0) FROM tasks WHERE owner_id=$1", string(a)).Scan(&count, &version); err != nil || count != wantCount || version != wantVersion {
				t.Error("Redis delete ran before another connection could see committed task state")
			}
			return nil
		}
		created, err := svc.CreateToday(ctx, a, applicationtask.CreateTodayInput{PlanDate: "2026-11-05", Title: "Committed", Priority: 2})
		if err != nil || len(read(t, svc, a).Items) != 1 {
			t.Fatal("create/read failed", err)
		}
		wantVersion = 2
		completed := true
		if _, err := svc.Update(ctx, a, created.ID, applicationtask.UpdateInput{Version: &created.Version, Completed: &completed}); err != nil || !read(t, svc, a).Items[0].Completed {
			t.Fatal("update/read failed", err)
		}
		wantCount, wantVersion = 0, 0
		if err := svc.Delete(ctx, a, created.ID); err != nil || len(read(t, svc, a).Items) != 0 || store.deletes != 3 {
			t.Fatal("delete/read failed", err)
		}
	})

	t.Run("generator releases transaction and deduplicates owner date", func(t *testing.T) {
		svc, store := setup(t)
		createRule(t, a)
		createRule(t, a)
		createRule(t, b)
		_ = read(t, svc, a)
		_ = read(t, svc, b)
		calls := map[domainaccount.UserID]int{}
		generator := applicationrecurrence.NewGenerator(infrarecurrence.New(workerPool), func(batch context.Context, owner domainaccount.UserID, date string) error {
			calls[owner]++
			probe, stop := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer stop()
			var count int
			// The worker has ONE connection. This read would block if GenerateBatch
			// still held its transaction or pooled connection during the callback.
			if err := workerPool.QueryRow(probe, "SELECT count(*) FROM tasks WHERE owner_id=$1", string(owner)).Scan(&count); err != nil || (owner == a && count != 2) || (owner == b && count != 1) || date != "2026-11-05" {
				t.Error("invalidation ran before commit/release or selected wrong owner/date")
			}
			return svc.InvalidateToday(batch, owner, date)
		})
		result, err := generator.GenerateBatch(ctx, clock)
		if err != nil || result.Generated != 3 || calls[a] != 1 || calls[b] != 1 || store.deletes != 2 || len(store.values) != 0 {
			t.Fatal("generated instances did not invalidate once per owner/date", err)
		}
		if len(read(t, svc, a).Items) != 2 || len(read(t, svc, b).Items) != 1 {
			t.Fatal("committed generated tasks not visible")
		}
		if result, err := generator.GenerateBatch(ctx, clock); err != nil || result.Generated != 0 || store.deletes != 2 {
			t.Fatal("no-op scan invalidated cache", err)
		}
	})

	t.Run("commit failures never delete warm entries", func(t *testing.T) {
		svc, store := setup(t)
		createRule(t, a)
		_ = read(t, svc, a)
		if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_cache_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture commit failure'; END $$;
CREATE CONSTRAINT TRIGGER cache_commit_failure AFTER INSERT ON tasks DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_cache_commit()`); err != nil {
			t.Fatal("cannot install deferred commit failure")
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DROP TRIGGER cache_commit_failure ON tasks; DROP FUNCTION fail_cache_commit()")
		})
		if _, err := svc.CreateToday(ctx, a, applicationtask.CreateTodayInput{PlanDate: "2026-11-05", Title: "Must roll back", Priority: 1}); err == nil {
			t.Fatal("task commit failure ignored")
		}
		generator := applicationrecurrence.NewGenerator(infrarecurrence.New(workerPool), svc.InvalidateToday)
		if _, err := generator.GenerateBatch(ctx, clock); err == nil {
			t.Fatal("generation commit failure ignored")
		}
		var tasks, occurrences int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM recurrence_occurrences)").Scan(&tasks, &occurrences); err != nil || tasks != 0 || occurrences != 0 || store.deletes != 0 || len(store.values) != 1 {
			t.Fatal("rollback mutated tasks/ledger/cache")
		}
	})

	t.Run("Redis failure preserves generator commit", func(t *testing.T) {
		svc, store := setup(t)
		createRule(t, a)
		_ = read(t, svc, a)
		store.onDelete = func(context.Context, string) error { return applicationtask.ErrUnavailable }
		generator := applicationrecurrence.NewGenerator(infrarecurrence.New(workerPool), svc.InvalidateToday)
		result, err := generator.GenerateBatch(ctx, clock)
		if err != nil || result.Generated != 1 || store.deletes != 1 {
			t.Fatal("Redis failure converted committed generation into failure", err)
		}
		if page := read(t, svc, a); len(page.Items) != 0 {
			t.Fatal("fixture should demonstrate bounded stale cache")
		}
		now = now.Add(16 * time.Second)
		if page := read(t, svc, a); len(page.Items) != 1 {
			t.Fatal("committed task not visible after stale deadline")
		}
		if result, err := generator.GenerateBatch(ctx, clock); err != nil || result.Generated != 0 || store.deletes != 1 {
			t.Fatal("follow-up scan regenerated tasks or invalidated unchanged data")
		}
	})

	t.Run("shared writes keep actor scope and never enter Today", func(t *testing.T) {
		_, store := setup(t)
		pid := "00000000-0000-4000-8000-000000000042"
		if _, err := pool.Exec(ctx, "INSERT INTO projects(id,owner_id,name) VALUES ($1,$2,'Shared fixture')", pid, string(a)); err != nil {
			t.Fatal("cannot create shared fixture")
		}
		if _, err := pool.Exec(ctx, "INSERT INTO project_memberships(project_id,user_id) VALUES ($1,$2)", pid, string(b)); err != nil {
			t.Fatal("cannot create member fixture")
		}
		svc := applicationtask.NewWithClock(infratask.New(pool), infraproject.New(pool), clock).WithTodayCache(store)
		created, err := svc.Create(ctx, a, applicationtask.CreateInput{Title: "Shared", Priority: 2, ProjectID: &pid})
		if err != nil {
			t.Fatal(err)
		}
		_ = read(t, svc, a)
		_ = read(t, svc, b)
		store.deletes = 0
		store.onDelete = func(_ context.Context, key string) error {
			if !strings.Contains(key, string(b)) {
				t.Error("member write invalidated task creator instead of actor")
			}
			return nil
		}
		title := "Updated by shared member"
		if _, err := svc.Update(ctx, b, created.ID, applicationtask.UpdateInput{Version: &created.Version, Title: &title}); err != nil || store.deletes != 1 || len(store.values) != 1 {
			t.Fatal("shared edit or actor-scoped invalidation changed", err)
		}
		if len(read(t, svc, a).Items) != 0 || len(read(t, svc, b).Items) != 0 {
			t.Fatal("project task leaked into personal Today")
		}
		page, err := svc.List(ctx, a, applicationtask.ListOptions{ListFilter: domaintask.ListFilter{ProjectID: &pid}})
		if err != nil || len(page.Items) != 1 || page.Items[0].Title != title {
			t.Fatal("original shared query behavior changed", err)
		}
	})
}
