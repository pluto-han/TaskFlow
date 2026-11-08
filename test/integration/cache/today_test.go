package cache_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
)

type todayReadCounter struct {
	task.Repository
	reads int
}

func (r *todayReadCounter) FindPage(ctx context.Context, owner session.UserID, query task.PageQuery) ([]task.Task, error) {
	r.reads++
	return r.Repository.FindPage(ctx, owner, query)
}

func TestRedisTodayServiceIntegration(t *testing.T) {
	client, _, marker := redisFixture(t)
	repo := &todayReadCounter{Repository: task.NewMemoryRepository()}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := task.NewServiceWithClock(repo, nil, func() time.Time { return now }).WithTodayCache(client)
	owner := session.UserID("00000000-0000-4000-8000-" + marker[:12])
	empty, err := svc.ListToday(context.Background(), owner, task.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A different owner gives the task fixture its own key without implementing
	// write invalidation, which belongs to Lesson 40.
	other := session.UserID("00000000-0000-4000-8001-" + marker[:12])
	if _, err := svc.CreateToday(context.Background(), other, task.CreateTodayTaskRequest{PlanDate: empty.PlanDate, Title: "Redis Today fixture", Priority: 2}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.ListToday(context.Background(), other, task.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	second, err := svc.ListToday(context.Background(), other, task.ListOptions{Limit: 20})
	if err != nil || repo.reads != 2 || len(second.Items) != 1 || second.Items[0].OwnerID != other || !second.ServerTime.Equal(now) {
		t.Fatal("real Redis did not preserve the Today hit contract", err)
	}
	a, _ := json.Marshal(first.Items)
	b, _ := json.Marshal(second.Items)
	if string(a) != string(b) || first.NextCursor != second.NextCursor {
		t.Fatal("real Redis serialization changed public data")
	}
	if _, err := svc.ListToday(context.Background(), owner, task.ListOptions{}); err != nil || repo.reads != 2 {
		t.Fatal("empty real Redis entry did not hit", err)
	}
}
