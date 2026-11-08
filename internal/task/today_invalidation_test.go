package task

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/cache"
	"github.com/pluto-han/taskflow/internal/session"
)

func TestTodayCacheSuccessfulWritesInvalidate(t *testing.T) {
	for _, action := range []string{"today create", "ordinary create", "edit", "complete", "delete"} {
		t.Run(action, func(t *testing.T) {
			svc, repo, store, _ := todayCacheFixture(t, 2)
			first := readCachedToday(t, svc, testOwner)
			other := readCachedToday(t, svc, otherOwner)
			key := todayCacheKey(testOwner, first.PlanDate)
			otherKey := todayCacheKey(otherOwner, other.PlanDate)
			otherBefore := string(store.values[otherKey])
			target := first.Items[0]
			title, complete := "Updated task", true
			wantCount := 2
			var err error
			switch action {
			case "today create":
				_, err = svc.CreateToday(context.Background(), testOwner, CreateTodayTaskRequest{PlanDate: first.PlanDate, Title: "New Today", Priority: 2})
				wantCount = 3
			case "ordinary create":
				_, err = svc.Create(context.Background(), testOwner, CreateTaskRequest{Title: "Ordinary", Priority: 2})
			case "edit":
				_, err = svc.Update(context.Background(), testOwner, target.ID, UpdateTaskRequest{Version: &target.Version, Title: &title})
			case "complete":
				_, err = svc.Update(context.Background(), testOwner, target.ID, UpdateTaskRequest{Version: &target.Version, Completed: &complete})
			case "delete":
				err = svc.Delete(context.Background(), testOwner, target.ID)
				wantCount = 1
			}
			if err != nil || store.deletes != 1 || store.deletedKeys[0] != key {
				t.Fatal("successful write did not invalidate exactly the actor's Today key", err)
			}
			if _, exists := store.values[key]; exists || string(store.values[otherKey]) != otherBefore {
				t.Fatal("actor entry remained or another owner's entry was removed")
			}
			fresh := readCachedToday(t, svc, testOwner)
			if len(fresh.Items) != wantCount || repo.reads != 3 {
				t.Fatal("write then read did not refresh from repository")
			}
			for _, item := range fresh.Items {
				if item.ID == target.ID {
					if action == "edit" && (item.Title != title || item.Version != 2) {
						t.Fatal("edit/version not visible")
					}
					if action == "complete" && (!item.Completed || item.Version != 2) {
						t.Fatal("completion/version not visible")
					}
					if action == "delete" {
						t.Fatal("deleted item remained")
					}
				}
			}
			_ = readCachedToday(t, svc, otherOwner)
			if repo.reads != 3 {
				t.Fatal("another user's cache no longer hit")
			}
		})
	}
}

func TestTodayCacheTwentyFirstTaskChangesPagination(t *testing.T) {
	svc, _, store, now := todayCacheFixture(t, 20)
	first := readCachedToday(t, svc, testOwner)
	if first.NextCursor != "" {
		t.Fatal("20 tasks unexpectedly have another page")
	}
	*now = now.Add(time.Second)
	created, err := svc.CreateToday(context.Background(), testOwner, CreateTodayTaskRequest{PlanDate: first.PlanDate, Title: "Twenty first", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := readCachedToday(t, svc, testOwner)
	if len(page.Items) != 20 || page.NextCursor == "" || page.Items[0].ID != created.ID || store.deletes != 1 {
		t.Fatal("21st task did not update first page and cursor")
	}
	if err := svc.Delete(context.Background(), testOwner, created.ID); err != nil {
		t.Fatal(err)
	}
	if page := readCachedToday(t, svc, testOwner); page.NextCursor != "" || len(page.Items) != 20 {
		t.Fatal("delete did not remove the stale next-page cursor")
	}
}

type failingTaskWrites struct {
	Repository
}

var errFixtureWrite = errors.New("fixture write failed")

func (f failingTaskWrites) Save(context.Context, session.UserID, *Task) error {
	return errFixtureWrite
}
func (f failingTaskWrites) Update(context.Context, session.UserID, *Task) error {
	return errFixtureWrite
}
func (f failingTaskWrites) Delete(context.Context, session.UserID, string) error {
	return errFixtureWrite
}

func TestTodayCacheFailedWritesDoNotInvalidate(t *testing.T) {
	svc, repo, store, _ := todayCacheFixture(t, 1)
	first := readCachedToday(t, svc, testOwner)
	item := first.Items[0]
	title := "Change"
	if _, err := svc.Update(context.Background(), otherOwner, item.ID, UpdateTaskRequest{Version: &item.Version, Title: &title}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatal("foreign write accepted")
	}
	if _, err := svc.Update(context.Background(), testOwner, item.ID, UpdateTaskRequest{Version: versionPointer(2), Title: &title}); !errors.Is(err, ErrTaskConflict) {
		t.Fatal("stale version accepted")
	}
	if err := svc.Delete(context.Background(), otherOwner, item.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatal("foreign delete accepted")
	}
	if _, err := svc.CreateToday(context.Background(), testOwner, CreateTodayTaskRequest{PlanDate: first.PlanDate, Title: " ", Priority: 1}); !errors.Is(err, ErrInvalidTask) {
		t.Fatal("invalid create accepted")
	}
	repo.Repository = failingTaskWrites{Repository: repo.Repository}
	if _, err := svc.CreateToday(context.Background(), testOwner, CreateTodayTaskRequest{PlanDate: first.PlanDate, Title: "New", Priority: 1}); !errors.Is(err, errFixtureWrite) {
		t.Fatal("save failure hidden")
	}
	if _, err := svc.Update(context.Background(), testOwner, item.ID, UpdateTaskRequest{Version: &item.Version, Title: &title}); !errors.Is(err, errFixtureWrite) {
		t.Fatal("update failure hidden")
	}
	if err := svc.Delete(context.Background(), testOwner, item.ID); !errors.Is(err, errFixtureWrite) {
		t.Fatal("delete failure hidden")
	}
	if store.deletes != 0 || len(store.values) != 1 {
		t.Fatal("a rejected or failed write invalidated cache")
	}
}

func TestTodayCacheDeleteFailureDoesNotFailCommittedWrite(t *testing.T) {
	for _, action := range []string{"create", "update", "delete"} {
		t.Run(action, func(t *testing.T) {
			svc, _, store, now := todayCacheFixture(t, 1)
			first := readCachedToday(t, svc, testOwner)
			item := first.Items[0]
			store.deleteErr = cache.ErrUnavailable
			wantCount := 1
			var err error
			switch action {
			case "create":
				_, err = svc.CreateToday(context.Background(), testOwner, CreateTodayTaskRequest{PlanDate: first.PlanDate, Title: "New", Priority: 1})
				wantCount = 2
			case "update":
				changed := "Updated despite Redis failure"
				_, err = svc.Update(context.Background(), testOwner, item.ID, UpdateTaskRequest{Version: &item.Version, Title: &changed})
			case "delete":
				err = svc.Delete(context.Background(), testOwner, item.ID)
				wantCount = 0
			}
			if err != nil || store.deletes != 1 {
				t.Fatal("Redis failure converted a committed write into an error", err)
			}
			*now = now.Add(16 * time.Second)
			fresh := readCachedToday(t, svc, testOwner)
			if len(fresh.Items) != wantCount || (action == "update" && fresh.Items[0].Title != "Updated despite Redis failure") {
				t.Fatal("TTL did not bound the stale result")
			}
		})
	}
}

type cancelAfterSave struct {
	Repository
	cancel context.CancelFunc
}

func (r cancelAfterSave) Save(ctx context.Context, owner session.UserID, item *Task) error {
	if err := r.Repository.Save(ctx, owner, item); err != nil {
		return err
	}
	r.cancel()
	return nil
}

func TestTodayCacheInvalidationSurvivesRequestCancellation(t *testing.T) {
	svc, repo, store, _ := todayCacheFixture(t, 1)
	first := readCachedToday(t, svc, testOwner)
	type requestKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), requestKey{}, "fixture-request"))
	defer cancel()
	repo.Repository = cancelAfterSave{Repository: repo.Repository, cancel: cancel}
	store.deleteHook = func(cleanup context.Context, key string) {
		deadline, ok := cleanup.Deadline()
		if cleanup.Err() != nil || !ok || time.Until(deadline) > cache.OperationTimeout || cleanup.Value(requestKey{}) != "fixture-request" {
			t.Error("cleanup lost its bounded, detached request context")
		}
		if key != todayCacheKey(testOwner, first.PlanDate) {
			t.Error("unexpected invalidation target")
		}
	}
	if _, err := svc.CreateToday(ctx, testOwner, CreateTodayTaskRequest{PlanDate: first.PlanDate, Title: "Committed before cancel", Priority: 1}); err != nil || ctx.Err() == nil || store.deletes != 1 {
		t.Fatal("confirmed commit was failed or not invalidated after cancellation", err)
	}
	if len(readCachedToday(t, svc, testOwner).Items) != 2 {
		t.Fatal("committed write not visible")
	}
}

type snapshotBarrier struct {
	Repository
	captured chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (r *snapshotBarrier) FindPage(ctx context.Context, owner session.UserID, query PageQuery) ([]Task, error) {
	rows, err := r.Repository.FindPage(ctx, owner, query)
	first := false
	r.once.Do(func() { first = true; close(r.captured) })
	if first {
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return rows, err
}

func TestTodayCacheOldReadAfterDeleteHasBoundedLifetime(t *testing.T) {
	svc, repo, store, now := todayCacheFixture(t, 1)
	started := *now
	rows, err := repo.Repository.FindPage(context.Background(), testOwner, PageQuery{Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatal("missing race fixture")
	}
	item := rows[0]
	barrier := &snapshotBarrier{Repository: repo.Repository, captured: make(chan struct{}), release: make(chan struct{})}
	repo.Repository = barrier
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(barrier.release) }) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := svc.ListToday(ctx, testOwner, ListOptions{})
		done <- err
	}()
	select {
	case <-barrier.captured:
	case <-ctx.Done():
		t.Fatal("reader failed to capture old snapshot")
	}
	*now = started.Add(10 * time.Second)
	title := "Committed while read was in flight"
	if _, err := svc.Update(context.Background(), testOwner, item.ID, UpdateTaskRequest{Version: &item.Version, Title: &title}); err != nil || store.deletes != 1 {
		t.Fatal("concurrent write failed", err)
	}
	release.Do(func() { close(barrier.release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// A writes its old result AFTER B's successful DEL. This is expected in the
	// first-version consistency model; it must never get another full 15 seconds.
	stale := readCachedToday(t, svc, testOwner)
	if stale.Items[0].Title == title || store.lastTTL > 5*time.Second {
		t.Fatal("race fixture or remaining TTL is incorrect")
	}
	var entry todayCacheEntry
	if err := json.Unmarshal(store.values[todayCacheKey(testOwner, stale.PlanDate)], &entry); err != nil || !entry.ReadStartedAt.Equal(started) || entry.ValidUntil.After(started.Add(todayCacheMaxAge)) {
		t.Fatal("old refill extended the absolute stale window")
	}
	*now = started.Add(todayCacheMaxAge)
	if fresh := readCachedToday(t, svc, testOwner); fresh.Items[0].Title != title {
		t.Fatal("old refill survived the absolute deadline")
	}
}
