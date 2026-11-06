package recurrence

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/testdb"
)

func ptr[T any](v T) *T { return &v }
func TestPostgresIntegration(t *testing.T) {
	raw := os.Getenv("TASKFLOW_RECURRENCE_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run scripts/test_integration.py")
	}
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson31_test" {
		t.Fatal("isolated lesson31 database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	a := session.UserID("00000000-0000-4000-8000-000000000001")
	b := session.UserID("00000000-0000-4000-8000-000000000002")
	exec("INSERT INTO users(id,email,password_hash) VALUES ($1,'a@example.com','test'),($2,'b@example.com','test')", string(a), string(b))
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s := NewServiceWithClock(NewPostgresRepository(pool), func() time.Time { return now })
	in := CreateRequest{Title: "Read", Priority: 2, AnchorDate: "2026-11-05", IntervalDays: 3}
	for _, bad := range []CreateRequest{
		{Title: strings.Repeat("字", 201), Priority: 2, AnchorDate: in.AnchorDate, IntervalDays: 3},
		{Title: "x", Description: strings.Repeat("x", 2001), Priority: 2, AnchorDate: in.AnchorDate, IntervalDays: 3},
		{Title: "x", Priority: 2, AnchorDate: "2026-11-04", IntervalDays: 3},
	} {
		if _, e := s.Create(ctx, a, bad); e != ErrInvalid {
			t.Fatal(e)
		}
	}
	rule, e := s.Create(ctx, a, in)
	if e != nil {
		t.Fatal(e)
	}
	if rule.Version != 1 || rule.PreviewDates[1] != "2026-11-08" {
		t.Fatal(rule)
	}
	if _, e = s.Get(ctx, b, rule.ID); e != ErrNotFound {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, b, rule.ID, UpdateRequest{Version: 1, Title: ptr("steal")}); e != ErrNotFound {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, b, rule.ID, 1); e != ErrNotFound {
		t.Fatal(e)
	}
	page, e := s.List(ctx, b, "", 20)
	if e != nil || len(page.Items) != 0 {
		t.Fatal(page, e)
	}
	if _, e = s.Update(ctx, a, rule.ID, UpdateRequest{Version: 1, IntervalDays: ptr(7)}); e != ErrInvalid {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Update(ctx, a, rule.ID, UpdateRequest{Version: 1, Title: ptr("Changed")})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else if e == ErrConflict {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	var tasks int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&tasks); e != nil || tasks != 0 {
		t.Fatal("CRUD generated tasks", tasks, e)
	}
	rule, e = s.Update(ctx, a, rule.ID, UpdateRequest{Version: 2, Paused: ptr(true)})
	if e != nil || len(rule.PreviewDates) != 0 {
		t.Fatal(rule, e)
	}
	exec("INSERT INTO recurrence_occurrences(rule_id,owner_id,scheduled_date) VALUES ($1,$2,'2026-11-08')", rule.ID, string(a))
	now = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	rule, e = s.Update(ctx, a, rule.ID, UpdateRequest{Version: rule.Version, Paused: ptr(false)})
	if e != nil || rule.NextRunDate != "2026-11-11" {
		t.Fatal(rule, e)
	}
	expectSQL := func(code, query string, args ...any) {
		t.Helper()
		_, e := pool.Exec(ctx, query, args...)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != code {
			t.Fatalf("want %s: %v", code, e)
		}
	}
	expectSQL("23505", "INSERT INTO recurrence_occurrences(rule_id,owner_id,scheduled_date) VALUES ($1,$2,'2026-11-08')", rule.ID, string(a))
	expectSQL("23503", "INSERT INTO recurrence_occurrences(rule_id,owner_id,scheduled_date) VALUES ($1,$2,'2026-11-11')", rule.ID, string(b))
	taskSQL := "INSERT INTO tasks(id,title,priority,owner_id,plan_date,recurrence_id) VALUES ($1,'snapshot',2,$2,$3,$4)"
	expectSQL("23503", taskSQL, "wrong-owner", string(b), "2026-11-08", rule.ID)
	exec(taskSQL, "instance", string(a), "2026-11-08", rule.ID)
	expectSQL("23505", taskSQL, "duplicate", string(a), "2026-11-08", rule.ID)
	expectSQL("23503", taskSQL, "wrong-day", string(a), "2026-11-09", rule.ID)
	expectSQL("23514", taskSQL, "no-plan", string(a), nil, rule.ID)
	rule, e = s.Update(ctx, a, rule.ID, UpdateRequest{Version: rule.Version, Title: ptr("future title"), AnchorDate: ptr("2026-11-12"), IntervalDays: ptr(7)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, a, rule.ID, rule.Version); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, a, rule.ID); e != ErrNotFound {
		t.Fatal(e)
	}
	var title string
	if e = pool.QueryRow(ctx, "SELECT title FROM tasks WHERE id='instance'").Scan(&title); e != nil || title != "snapshot" {
		t.Fatal(title, e)
	}
	exec("DELETE FROM tasks WHERE id='instance'")
	var receipts int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM recurrence_occurrences WHERE rule_id=$1", rule.ID).Scan(&receipts); e != nil || receipts != 1 {
		t.Fatal(receipts, e)
	}
	// Keyset pages bind to the authenticated owner and exclude deleted rules.
	in.AnchorDate = "2026-11-08"
	for range 3 {
		if _, e = s.Create(ctx, a, in); e != nil {
			t.Fatal(e)
		}
	}
	page, e = s.List(ctx, a, "", 2)
	if e != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal(page, e)
	}
	if _, e = s.List(ctx, b, page.NextCursor, 2); e != ErrInvalid {
		t.Fatal(e)
	}
	next, e := s.List(ctx, a, page.NextCursor, 2)
	if e != nil || len(next.Items) != 1 || next.NextCursor != "" {
		t.Fatal(next, e)
	}
	if next.Items[0].ID == page.Items[1].ID {
		t.Fatal("duplicate page")
	}
}
