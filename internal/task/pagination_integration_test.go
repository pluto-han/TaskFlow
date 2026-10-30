package task

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pluto-han/taskflow/internal/session"
)

// Capture the generated query and parameters, so EXPLAIN measures the actual
// repository query rather than a separately maintained SQL approximation.
type listQueryTrace struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (tr *listQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: ListTasks") {
		tr.mu.Lock()
		tr.sql = data.SQL
		tr.args = append([]any(nil), data.Args...)
		tr.mu.Unlock()
	}
	return ctx
}
func (*listQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestTaskPaginationIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_PAGINATION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TASKFLOW_PAGINATION_TEST_DATABASE_URL to an isolated migrated taskflow_lesson15_test database")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson15_test" {
		t.Fatal("requires isolated loopback taskflow_lesson15_test database")
	}
	trace := &listQueryTrace{}
	cfg.ConnConfig.Tracer = trace
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 9 || dirty {
		t.Fatal("unexpected schema state")
	}
	// Atomic insert fails on pre-existing fixture IDs: never clean another run's rows.
	_, err = pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES ($1,'lesson15-a@example.com','test-only'),($2,'lesson15-b@example.com','test-only')", string(testOwner), string(otherOwner))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := pool.Exec(clean, "DELETE FROM tasks WHERE owner_id IN ($1,$2)", string(testOwner), string(otherOwner)); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(clean, "DELETE FROM users WHERE id IN ($1,$2)", string(testOwner), string(otherOwner)); err != nil {
			t.Error(err)
		}
	}()
	repo := NewPostgresRepository(pool)
	paginationContract(t, repo, testOwner, otherOwner)
	dueDateContract(t, repo)
	// Clear vs. set read the same version, then race at PostgreSQL's atomic UPDATE.
	service := NewService(repo, nil)
	task, err := service.Create(ctx, testOwner, CreateTaskRequest{Title: "Deadline race", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	synced := &synchronizedReadRepository{Repository: repo}
	synced.readers.Add(2)
	raceService := NewService(synced, nil)
	due := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, value := range []*time.Time{nil, &due} {
		wg.Add(1)
		go func(value *time.Time) {
			defer wg.Done()
			_, err := raceService.Update(ctx, testOwner, task.ID, UpdateTaskRequest{DueDate: OptionalTime{Present: true, Value: value}, Version: versionPointer(1)})
			results <- err
		}(value)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrTaskConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("deadline race did not produce one success and one conflict")
	}
	// Explain a modest synthetic dataset, not a production capacity benchmark.
	_, err = pool.Exec(ctx, `INSERT INTO tasks(id,owner_id,title,priority,completed,created_at,due_date)
 SELECT 'explain-'||lpad(g::text,6,'0'),$1,'Plan fixture',g%3+1,g%2=0,
 '2025-06-01'::timestamptz-g*interval '1 second',
 '2025-06-01'::timestamptz+(g%100)*interval '1 day'
 FROM generate_series(1,5000) g`, string(testOwner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE tasks"); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"first page", "cursor page", "filtered cursor page", "deadline range"} {
		options := ListOptions{Limit: 20}
		if scenario == "filtered cursor page" {
			options.Completed = boolPointer(false)
			options.Priority = intPointer(2)
		}
		if scenario == "deadline range" {
			from := time.Date(2025, 6, 21, 0, 0, 0, 0, time.UTC)
			before := from.Add(24 * time.Hour)
			options.DueFrom = &from
			options.DueBefore = &before
		}
		if scenario != "first page" {
			page, err := service.List(ctx, testOwner, options)
			if err != nil {
				t.Fatal(err)
			}
			options.Cursor = page.NextCursor
		}
		if _, err := service.List(ctx, testOwner, options); err != nil {
			t.Fatal(err)
		}
		trace.mu.Lock()
		sql, args := trace.sql, append([]any(nil), trace.args...)
		trace.mu.Unlock()
		rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s:\n%s", scenario, strings.Join(lines, "\n"))
	}
	// Both direct repositories still require the authenticated subject.
	if _, err := repo.FindPage(ctx, session.UserID(""), PageQuery{Limit: 21}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal("missing owner accepted")
	}
}
