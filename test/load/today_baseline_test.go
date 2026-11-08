package load_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
)

const (
	baselineOwner session.UserID = "00000000-0000-4000-8000-000000000037"
	secondOwner   session.UserID = "00000000-0000-4000-8000-000000000038"
)

// Count only the target list read. Writes, item reads and authentication are
// deliberately outside this counter; memory timings are not PostgreSQL timings.
type countingRepository struct {
	task.Repository
	findPages atomic.Int64
}

func (r *countingRepository) FindPage(ctx context.Context, owner session.UserID, query task.PageQuery) ([]task.Task, error) {
	r.findPages.Add(1)
	return r.Repository.FindPage(ctx, owner, query)
}

func todayFixture(tb testing.TB) (*task.Service, *countingRepository, *time.Time) {
	tb.Helper()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	repo := &countingRepository{Repository: task.NewMemoryRepository()}
	day := planning.At(now).PlanDate
	yesterday := "2026-11-04"
	for _, seed := range []struct {
		owner session.UserID
		name  string
		count int
		date  *string
	}{
		{baselineOwner, "today-a", 24, &day},
		{secondOwner, "today-b", 8, &day},
		{baselineOwner, "history", 3, &yesterday},
		{baselineOwner, "ordinary", 4, nil},
	} {
		for i := 0; i < seed.count; i++ {
			item := task.Task{ID: fmt.Sprintf("baseline-%s-%02d", seed.name, i),
				Title: "Baseline task", Priority: i%3 + 1, Completed: i%4 == 0,
				CreatedAt: now.Add(time.Duration(i) * time.Microsecond), PlanDate: seed.date}
			if i%2 == 0 {
				due := now.Add(time.Hour)
				item.DueDate = &due
			}
			if err := repo.Save(context.Background(), seed.owner, &item); err != nil {
				tb.Fatal(err)
			}
		}
	}
	return task.NewServiceWithClock(repo, nil, func() time.Time { return now }), repo, &now
}

func checkToday(tb testing.TB, page task.TodayPage, owner session.UserID, count int) {
	tb.Helper()
	if len(page.Items) != count || page.PlanDate != "2026-11-05" || page.Timezone != planning.Timezone {
		tb.Fatalf("unexpected page: count=%d date=%s timezone=%s", len(page.Items), page.PlanDate, page.Timezone)
	}
	for _, item := range page.Items {
		if item.OwnerID != owner || item.PlanDate == nil || *item.PlanDate != page.PlanDate || item.ProjectID != nil {
			tb.Fatal("today result crossed owner, date or project boundary")
		}
	}
}

// This is the cache-disabled comparison group for later lessons. Both scenarios
// issue 100 reads; neither currently avoids a FindPage call.
func TestTodayUncachedBaseline(t *testing.T) {
	for _, scenario := range []string{"same-user", "alternating-two-users"} {
		t.Run(scenario, func(t *testing.T) {
			svc, repo, now := todayFixture(t)
			for i := 0; i < 100; i++ {
				owner, count := baselineOwner, 20
				if scenario == "alternating-two-users" && i%2 == 1 {
					owner, count = secondOwner, 8
				}
				page, err := svc.ListToday(context.Background(), owner, task.ListOptions{Limit: 20})
				if err != nil {
					t.Fatal(err)
				}
				checkToday(t, page, owner, count)
				if !page.ServerTime.Equal(*now) || (page.NextCursor != "") != (owner == baselineOwner) {
					t.Fatal("unexpected time or pagination metadata")
				}
			}
			if calls := repo.findPages.Load(); calls != 100 {
				t.Fatalf("100 uncached reads made %d FindPage calls; want 100", calls)
			}
			t.Log("cache=absent backend=memory requests=100 successes=100 find_page_calls=100 calls_per_request=1")
		})
	}
}

// Distinguish valid requests outside the future cache scope from invalid ones.
// This does not implement cache eligibility or introduce a Redis dependency.
func TestTodayUncachedQueryVariants(t *testing.T) {
	svc, repo, _ := todayFixture(t)
	first, err := svc.ListToday(context.Background(), baselineOwner, task.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, query string
		count       int
	}{
		{"default", "", 20},
		{"explicit default", "limit=20", 20},
		{"next page", "cursor=" + first.NextCursor, 4},
		{"different size", "limit=5", 5},
		{"completed", "completed=true", 6},
		{"incomplete", "completed=false", 18},
		{"priority", "priority=2", 8},
		{"with deadline", "has_due_date=true", 12},
		{"without deadline", "has_due_date=false", 12},
		{"deadline range", "due_from=2026-11-05T12:00:00Z&due_before=2026-11-05T14:00:00Z", 12},
		{"untrusted identity ignored", "owner_id=" + string(secondOwner), 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, err := task.ParseListOptions(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			before := repo.findPages.Load()
			page, err := svc.ListToday(context.Background(), baselineOwner, options)
			if err != nil {
				t.Fatal(err)
			}
			checkToday(t, page, baselineOwner, tc.count)
			if repo.findPages.Load()-before != 1 {
				t.Fatal("valid uncached request must make one FindPage call")
			}
		})
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=20&limit=20", "completed=", "priority=4", "project_id=none", "plan_date=2026-11-04", "cursor=invalid"} {
		t.Run("invalid/"+query, func(t *testing.T) {
			before := repo.findPages.Load()
			options, err := task.ParseListOptions(query)
			if err == nil {
				_, err = svc.ListToday(context.Background(), baselineOwner, options)
			}
			if !errors.Is(err, task.ErrInvalidList) || repo.findPages.Load() != before {
				t.Fatalf("invalid query reached repository or changed error: %v", err)
			}
		})
	}
	before := repo.findPages.Load()
	if _, err := svc.ListToday(context.Background(), "", task.ListOptions{}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal("missing identity accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.ListToday(ctx, baselineOwner, task.ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if repo.findPages.Load() != before {
		t.Fatal("invalid identity or cancelled request reached repository")
	}
}

func TestTodayUncachedDayBoundary(t *testing.T) {
	svc, repo, now := todayFixture(t)
	first, err := svc.ListToday(context.Background(), baselineOwner, task.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	*now = first.NextDayAt
	page, err := svc.ListToday(context.Background(), baselineOwner, task.ListOptions{})
	if err != nil || len(page.Items) != 0 || page.PlanDate != "2026-11-06" || !page.ServerTime.Equal(*now) {
		t.Fatal("new Amsterdam day returned stale items or time", err)
	}
	if repo.findPages.Load() != 2 {
		t.Fatal("both days must read the repository")
	}
	if _, err := svc.ListToday(context.Background(), baselineOwner, task.ListOptions{Cursor: first.NextCursor}); !errors.Is(err, task.ErrInvalidList) {
		t.Fatal("previous day's cursor accepted")
	}
	if repo.findPages.Load() != 2 {
		t.Fatal("invalid cursor reached repository")
	}
}

func BenchmarkTodayUncached(b *testing.B) {
	for _, scenario := range []string{"same-user", "alternating-two-users"} {
		b.Run(scenario, func(b *testing.B) {
			svc, repo, _ := todayFixture(b)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				owner := baselineOwner
				if scenario == "alternating-two-users" && i%2 == 1 {
					owner = secondOwner
				}
				if _, err := svc.ListToday(ctx, owner, task.ListOptions{Limit: 20}); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if repo.findPages.Load() != int64(b.N) {
				b.Fatal("baseline no longer makes one FindPage call per read")
			}
			b.ReportMetric(float64(repo.findPages.Load())/float64(b.N), "FindPage/op")
		})
	}
}
