package recurrence_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
	infradatabase "github.com/pluto-han/taskflow/internal/infra/database"
	infrarecurrence "github.com/pluto-han/taskflow/internal/infra/persistence/recurrence"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

func TestSchedulerPostgresIntegration(t *testing.T) {
	raw := os.Getenv("TASKFLOW_SCHEDULER_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run scripts/test_integration.py")
	}
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson32_test" {
		t.Fatal("isolated lesson32 database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	workerCfg, e := infradatabase.WorkerPoolConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	workerPool, e := pgxpool.NewWithConfig(ctx, workerCfg)
	if e != nil {
		t.Fatal(e)
	}
	defer workerPool.Close()
	repo := infrarecurrence.New(workerPool)
	adminRepo := infrarecurrence.New(pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if e := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	owner := domainaccount.UserID("00000000-0000-4000-8000-000000000032")
	exec("INSERT INTO users(id,email,password_hash)VALUES($1,'scheduler@example.com','test')", string(owner))
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := applicationrecurrence.NewWithClock(adminRepo, clock)
	create := func(interval int) *applicationrecurrence.RuleResult {
		t.Helper()
		r, e := service.Create(ctx, owner, applicationrecurrence.CreateInput{Title: "Snapshot", Description: "Description", Priority: 3, AnchorDate: "2026-11-05", IntervalDays: interval})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	reset := func() {
		exec("DELETE FROM tasks")
		exec("DELETE FROM recurrence_occurrences")
		exec("DELETE FROM recurrence_rules")
		now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	}
	run := func() applicationrecurrence.BatchResult {
		t.Helper()
		r, e := repo.GenerateBatch(ctx, clock)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	t.Run("snapshot restart deletion and weeks offline", func(t *testing.T) {
		reset()
		rule := create(3)
		if r := run(); r.Generated != 1 || r.Processed != 1 {
			t.Fatal(r)
		}
		if _, e := service.Update(ctx, owner, rule.ID, applicationrecurrence.UpdateInput{Version: 1, Title: ptr("stale edit")}); !errors.Is(e, domainrecurrence.ErrConflict) {
			t.Fatal("scheduler failed to advance version", e)
		}
		var title, description, date, source, next string
		var priority int
		var project *string
		if e := pool.QueryRow(ctx, "SELECT title,description,priority,plan_date::text,recurrence_id::text,project_id::text FROM tasks").Scan(&title, &description, &priority, &date, &source, &project); e != nil {
			t.Fatal(e)
		}
		if title != "Snapshot" || description != "Description" || priority != 3 || date != "2026-11-05" || source != rule.ID || project != nil {
			t.Fatal("invalid snapshot")
		}
		if r := run(); r.Generated != 0 {
			t.Fatal(r)
		}
		// Cursor replay after task deletion must not recreate the instance.
		exec("DELETE FROM tasks")
		exec("UPDATE recurrence_rules SET next_run_date=anchor_date WHERE id=$1", rule.ID)
		if r := run(); r.Generated != 0 || count("tasks") != 0 || count("recurrence_occurrences") != 1 {
			t.Fatal(r)
		}
		now = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
		if r := run(); r.Generated != 1 || count("tasks") != 1 || count("recurrence_occurrences") != 2 {
			t.Fatal(r)
		}
		if e := pool.QueryRow(ctx, "SELECT next_run_date::text FROM recurrence_rules WHERE id=$1", rule.ID).Scan(&next); e != nil || next != "2026-11-29" {
			t.Fatal(next, e)
		}
		// A fresh repository simulates a process restart without in-memory last-run state.
		result, e := infrarecurrence.New(workerPool).GenerateBatch(ctx, clock)
		if e != nil || result.Generated != 0 {
			t.Fatal(result, e)
		}
	})
	t.Run("skip past day without changing anchor", func(t *testing.T) {
		reset()
		rule := create(3)
		now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
		if r := run(); r.Generated != 0 || r.Processed != 1 {
			t.Fatal(r)
		}
		got, e := service.Get(ctx, owner, rule.ID)
		if e != nil || got.NextRunDate != "2026-11-11" || got.AnchorDate != "2026-11-05" {
			t.Fatal(got, e)
		}
	})
	t.Run("two schedulers bounded batches", func(t *testing.T) {
		reset()
		for range 45 {
			create(1)
		}
		secondPool, e := pgxpool.NewWithConfig(ctx, workerCfg.Copy())
		if e != nil {
			t.Fatal(e)
		}
		defer secondPool.Close()
		second := infrarecurrence.New(secondPool)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, worker := range []*infrarecurrence.Repository{repo, second} {
			wg.Add(1)
			go func(r *infrarecurrence.Repository) {
				defer wg.Done()
				for range 5 {
					result, e := r.GenerateBatch(ctx, clock)
					if e != nil {
						errs <- e
						return
					}
					if result.Processed > applicationrecurrence.BatchSize {
						errs <- errors.New("unbounded batch")
						return
					}
				}
			}(worker)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Fatal(e)
		}
		if count("tasks") != 45 || count("recurrence_occurrences") != 45 {
			t.Fatal("duplicates or missing tasks")
		}
		if workerPool.Stat().MaxConns() != 1 || secondPool.Stat().MaxConns() != 1 {
			t.Fatal("connection budget")
		}
	})
	t.Run("rollback after ledger insert and recover", func(t *testing.T) {
		reset()
		create(1)
		exec("CREATE FUNCTION fail_generated_task() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated insert failure'; END $$")
		exec("CREATE TRIGGER fail_generated BEFORE INSERT ON tasks FOR EACH ROW EXECUTE FUNCTION fail_generated_task()")
		_, e := repo.GenerateBatch(ctx, clock)
		if e == nil {
			t.Fatal("injected failure ignored")
		}
		if count("tasks") != 0 || count("recurrence_occurrences") != 0 {
			t.Fatal("partial commit")
		}
		exec("DROP TRIGGER fail_generated ON tasks")
		exec("DROP FUNCTION fail_generated_task()")
		if r := run(); r.Generated != 1 {
			t.Fatal(r)
		}
	})
	t.Run("midnight rolls back written batch", func(t *testing.T) {
		reset()
		create(1)
		calls := 0
		_, e := repo.GenerateBatch(ctx, func() time.Time {
			calls++
			if calls >= 4 {
				return now.AddDate(0, 0, 1)
			}
			return now
		})
		if !errors.Is(e, applicationrecurrence.ErrDayChanged) || count("tasks") != 0 || count("recurrence_occurrences") != 0 {
			t.Fatal(e)
		}
		now = now.AddDate(0, 0, 1)
		if r := run(); r.Generated != 1 {
			t.Fatal(r)
		}
	})
	t.Run("pause lock is shared with scheduler", func(t *testing.T) {
		reset()
		rule := create(1)
		locked := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			_, e := adminRepo.Change(ctx, owner, rule.ID, 1, func(r *domainrecurrence.Rule, _ string) error { close(locked); <-release; r.Paused = true; return nil })
			done <- e
		}()
		<-locked
		result, e := repo.GenerateBatch(ctx, clock)
		close(release)
		if changeErr := <-done; changeErr != nil {
			t.Fatal(changeErr)
		}
		if e != nil || result.Processed != 0 {
			t.Fatal(result, e)
		}
		if run().Generated != 0 || count("tasks") != 0 {
			t.Fatal("paused task generated")
		}
		if e = service.Delete(ctx, owner, rule.ID, 2); e != nil {
			t.Fatal(e)
		}
		if run().Generated != 0 {
			t.Fatal("deleted task generated")
		}
	})
	t.Run("Amsterdam midnight and DST retain civil day", func(t *testing.T) {
		reset()
		now = time.Date(2026, 3, 28, 23, 30, 0, 0, time.UTC) // Already March 29 in Amsterdam.
		_, e := service.Create(ctx, owner, applicationrecurrence.CreateInput{Title: "DST", Priority: 1, AnchorDate: "2026-03-29", IntervalDays: 1})
		if e != nil {
			t.Fatal(e)
		}
		if run().Generated != 1 {
			t.Fatal("DST date missing")
		}
		now = time.Date(2026, 3, 29, 22, 30, 0, 0, time.UTC) // March 30 after the spring offset change.
		if run().Generated != 1 || count("tasks") != 2 {
			t.Fatal("DST repeated/skipped day")
		}
		var days string
		if e = pool.QueryRow(ctx, "SELECT string_agg(plan_date::text,',' ORDER BY plan_date) FROM tasks").Scan(&days); e != nil || days != "2026-03-29,2026-03-30" {
			t.Fatal(days, e)
		}
	})
	t.Run("calendar exhaustion pauses rule", func(t *testing.T) {
		reset()
		now = time.Date(9999, 12, 31, 12, 0, 0, 0, time.UTC)
		rule, e := service.Create(ctx, owner, applicationrecurrence.CreateInput{Title: "Last day", Priority: 1, AnchorDate: "9999-12-31", IntervalDays: 1})
		if e != nil {
			t.Fatal(e)
		}
		if run().Generated != 1 {
			t.Fatal("last date missing")
		}
		saved, e := service.Get(ctx, owner, rule.ID)
		if e != nil || !saved.Paused {
			t.Fatal(saved, e)
		}
		if run().Generated != 0 {
			t.Fatal("exhausted rule repeated")
		}
	})
	t.Run("connection failure recovery", func(t *testing.T) {
		reset()
		create(1)
		killed := false
		_, e := repo.GenerateBatch(ctx, func() time.Time {
			if !killed {
				killed = true
				exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND application_name='taskflow-recurrence' AND pid<>pg_backend_pid()")
			}
			return now
		})
		if e == nil {
			t.Fatal("terminated connection reported success")
		}
		if run().Generated != 1 {
			t.Fatal("did not reconnect")
		}
	})
}
