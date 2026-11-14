package notification_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationnotification "github.com/pluto-han/taskflow/internal/application/notification"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	infranotification "github.com/pluto-han/taskflow/internal/infra/persistence/notification"
	infrarecurrence "github.com/pluto-han/taskflow/internal/infra/persistence/recurrence"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

// The suite starts with a populated v9 fixture and upgrades it before this test.
// Only this guarded disposable database shadows SQL now() via its search_path.
func TestLesson35TodayRecurrenceAndNotification(t *testing.T) {
	raw := os.Getenv("TASKFLOW_FLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run scripts/test_integration.py")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson35_test" {
		t.Fatal("isolated database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bootstrap, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.Exec(ctx, `CREATE SCHEMA lesson35_clock;
CREATE TABLE lesson35_clock.time(value timestamptz NOT NULL);
INSERT INTO lesson35_clock.time VALUES ('2026-11-05 12:00:00+00');
CREATE FUNCTION lesson35_clock.now() RETURNS timestamptz LANGUAGE SQL STABLE AS 'SELECT value FROM lesson35_clock.time LIMIT 1'`)
	bootstrap.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET search_path = lesson35_clock, pg_catalog, public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner := domainaccount.UserID("00000000-0000-4000-8000-000000000035")
	other := domainaccount.UserID("00000000-0000-4000-8000-000000000036")
	exec("INSERT INTO users(id,email,password_hash) VALUES ($1,'flow-owner@example.com','fixture'),($2,'flow-other@example.com','fixture')", string(owner), string(other))
	exec("INSERT INTO discord_bindings(owner_id,guild_id,channel_id,channel_name,discord_user_id,bound_at) VALUES($1,'111','222','test','333',now())", string(owner))
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	advance := func(days int) {
		t.Helper()
		now = now.AddDate(0, 0, days)
		exec("UPDATE lesson35_clock.time SET value=$1", now)
	}
	today := applicationtask.NewWithClock(infratask.New(pool), nil, clock)
	rules := applicationrecurrence.NewWithClock(infrarecurrence.New(pool), clock)
	day := domainplanning.At(now).PlanDate
	if _, err := today.CreateToday(ctx, owner, applicationtask.CreateTodayInput{PlanDate: day, Title: "manual", Priority: 2}); err != nil {
		t.Fatal(err)
	}
	_, err = rules.Create(ctx, owner, applicationrecurrence.CreateInput{Title: "repeat", Priority: 2, AnchorDate: day, IntervalDays: 3, ReminderEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	generator := infrarecurrence.New(pool)
	scan := func(expected int) {
		t.Helper()
		result, err := generator.GenerateBatch(ctx, clock)
		if err != nil || result.Generated != expected {
			t.Fatalf("generation %+v: %v", result, err)
		}
	}
	list := func(user domainaccount.UserID, expected int) {
		t.Helper()
		page, err := today.ListToday(ctx, user, applicationtask.ListOptions{})
		if err != nil || len(page.Items) != expected {
			t.Fatalf("today items %d want %d: %v", len(page.Items), expected, err)
		}
	}
	fake := &fakeSender{result: applicationnotification.SendResult{Status: 200}}
	repo := infranotification.New(pool, fake.CredentialFingerprint())
	sender := applicationnotification.NewWithClock(repo, fake, clock)
	send := func() {
		t.Helper()
		found, err := sender.ProcessOne(ctx)
		if err != nil || !found {
			t.Fatalf("send found=%v: %v", found, err)
		}
	}
	scan(1)
	scan(0)
	list(owner, 2)
	list(other, 0)
	send()
	advance(1)
	scan(0)
	list(owner, 0)
	advance(2)
	scan(1)
	scan(0)
	list(owner, 1)
	list(other, 0)
	first, err := repo.Claim(ctx)
	if err != nil || first == nil {
		t.Fatal("first lease", err)
	}
	now = now.Add(31 * time.Second)
	exec("UPDATE lesson35_clock.time SET value=$1", now)
	second, err := repo.Claim(ctx)
	if err != nil || second == nil || !second.Reclaimed || second.Token == first.Token {
		t.Fatal("lease was not reclaimed", err)
	}
	if err := repo.Finish(ctx, first, applicationnotification.Delivery{Status: "sent"}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, "SELECT status FROM notification_outbox WHERE id=$1", first.ID).Scan(&status); err != nil || status != "leased" {
		t.Fatal("stale lease changed event", status, err)
	}
	now = now.Add(31 * time.Second)
	exec("UPDATE lesson35_clock.time SET value=$1", now)
	fake.err = errors.New("simulated network failure")
	send()
	fake.err = nil
	if found, err := sender.ProcessOne(ctx); err != nil || found {
		t.Fatalf("retry must wait for fake clock: found=%v err=%v", found, err)
	}
	now = now.Add(2 * time.Minute)
	exec("UPDATE lesson35_clock.time SET value=$1", now)
	send()
	if fake.calls != 3 || len(fake.channels) != 3 || fake.channels[0] != "222" || fake.users[0] != "333" {
		t.Fatal("wrong delivery count or binding target")
	}
	var history int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE owner_id=$1", string(owner)).Scan(&history); err != nil || history != 3 {
		t.Fatalf("history %d: %v", history, err)
	}
	if events, err := sender.List(ctx, string(other)); err != nil || len(events) != 0 {
		t.Fatal("cross-user notification leak")
	}
}
