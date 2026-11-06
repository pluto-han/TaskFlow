package notification

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/discord"
	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/recurrence"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/testdb"
)

// The suite starts with a populated v9 fixture and upgrades it before this test.
// PostgreSQL lease timestamps still use the database clock: the future event is
// made due explicitly in this disposable database after advancing the calendar.
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
	owner := session.UserID("00000000-0000-4000-8000-000000000035")
	other := session.UserID("00000000-0000-4000-8000-000000000036")
	exec("INSERT INTO users(id,email,password_hash) VALUES ($1,'flow-owner@example.com','fixture'),($2,'flow-other@example.com','fixture')", string(owner), string(other))
	exec("INSERT INTO discord_bindings(owner_id,guild_id,channel_id,channel_name,discord_user_id,bound_at) VALUES($1,'111','222','test','333',now())", string(owner))
	now := time.Now()
	clock := func() time.Time { return now }
	today := task.NewServiceWithClock(task.NewPostgresRepository(pool), nil, clock)
	rules := recurrence.NewServiceWithClock(recurrence.NewPostgresRepository(pool), clock)
	day := planning.At(now).PlanDate
	if _, err := today.CreateToday(ctx, owner, task.CreateTodayTaskRequest{PlanDate: day, Title: "manual", Priority: 2}); err != nil {
		t.Fatal(err)
	}
	_, err = rules.Create(ctx, owner, recurrence.CreateRequest{Title: "repeat", Priority: 2, AnchorDate: day, IntervalDays: 3, ReminderEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	generator := recurrence.NewPostgresRepository(pool)
	scan := func(expected int) {
		t.Helper()
		result, err := generator.GenerateBatch(ctx, clock)
		if err != nil || result.Generated != expected {
			t.Fatalf("generation %+v: %v", result, err)
		}
	}
	list := func(user session.UserID, expected int) {
		t.Helper()
		page, err := today.ListToday(ctx, user, task.ListOptions{})
		if err != nil || len(page.Items) != expected {
			t.Fatalf("today items %d want %d: %v", len(page.Items), expected, err)
		}
	}
	fake := &fakeSender{result: discord.SendResult{Status: 200}}
	sender := NewService(pool, fake)
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
	now = now.AddDate(0, 0, 1)
	scan(0)
	list(owner, 0)
	now = now.AddDate(0, 0, 2)
	scan(1)
	scan(0)
	list(owner, 1)
	list(other, 0)
	// Only this isolated fixture is shifted: production DB clocks are untouched.
	exec("UPDATE notification_outbox SET next_attempt_at=now(),expires_at=now()+interval '1 hour' WHERE status='pending'")
	send()
	if fake.calls != 2 || len(fake.channels) != 2 || fake.channels[0] != "222" || fake.users[0] != "333" {
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
