package task

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/testdb"
)

func TestTodayPostgresIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_TODAY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("use scripts/test_integration.py for isolated lesson29 database")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson29_test" {
		t.Fatal("requires isolated loopback lesson29 database")
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
	if err = pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 14 || dirty {
		t.Fatal("requires clean schema 14")
	}
	_, err = pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES ($1,'today-a@example.com','test'),($2,'today-b@example.com','test')", string(testOwner), string(otherOwner))
	if err != nil {
		t.Fatal(err)
	}
	todayContract(t, NewPostgresRepository(pool))
	// SQL, not just Service, prevents moving planned tasks into shared projects.
	_, err = pool.Exec(ctx, "INSERT INTO projects(id,owner_id,name) VALUES ('00000000-0000-4000-8000-000000000099',$1,'Project')", string(testOwner))
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO tasks(id,title,priority,owner_id,plan_date,project_id) VALUES ('forbidden','x',1,$1,'2026-11-05','00000000-0000-4000-8000-000000000099')",
		"UPDATE tasks SET project_id='00000000-0000-4000-8000-000000000099' WHERE owner_id=$1 AND plan_date IS NOT NULL",
		"INSERT INTO tasks(id,title,priority,owner_id,plan_date) VALUES ('infinity','x',1,$1,'infinity')",
	} {
		_, err = pool.Exec(ctx, query, string(testOwner))
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("constraint not enforced: %v", err)
		}
	}
	// Original column-list SQL remains valid on the expanded schema (v9 reader/writer).
	_, err = pool.Exec(ctx, "INSERT INTO tasks(id,title,priority,owner_id) VALUES ('legacy-writer','Legacy',1,$1)", string(testOwner))
	if err != nil {
		t.Fatal(err)
	}
	var date *string
	var title string
	if err = pool.QueryRow(ctx, "SELECT title,plan_date::text FROM tasks WHERE id='legacy-writer'").Scan(&title, &date); err != nil || date != nil || title != "Legacy" {
		t.Fatal("legacy insert compatibility failed")
	}
	// Read a row seeded before migration; v9 sessions/projects are compared by runner.
	if err = pool.QueryRow(ctx, "SELECT title,plan_date::text FROM tasks WHERE id='upgrade-task'").Scan(&title, &date); err != nil || date != nil || title != "Preserved task" {
		t.Fatal("upgrade fixture lost")
	}
	// Query parameterization must not depend on the connection's own timezone.
	cfg.ConnConfig.RuntimeParams["timezone"] = "Pacific/Honolulu"
	foreignTZ, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer foreignTZ.Close()
	svc := NewServiceWithClock(NewPostgresRepository(foreignTZ), nil, todayTime)
	page, err := svc.ListToday(ctx, testOwner, ListOptions{})
	if err != nil || len(page.Items) != 1 || *page.Items[0].PlanDate != "2026-11-05" {
		t.Fatalf("connection timezone changed day: %+v %v", page, err)
	}
	// Date identity is immutable even through the repository interface.
	item := page.Items[0]
	changed := "2026-11-07"
	item.PlanDate = &changed
	if err = NewPostgresRepository(pool).Update(ctx, testOwner, &item); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("repository changed plan identity: %v", err)
	}
}
