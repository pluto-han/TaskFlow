package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/session"
)

func TestToDomainTaskPreservesVersion(t *testing.T) {
	got := toDomainTask(db.Task{ID: "task-1", Version: 7})
	if got.Version != 7 {
		t.Fatalf("version = %d, want 7", got.Version)
	}
}

func TestServiceUpdateRejectedVersionDoesNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version *int64
		want    error
	}{
		{"missing", nil, ErrInvalidUpdate},
		{"zero", versionPointer(0), ErrInvalidUpdate},
		{"negative", versionPointer(-1), ErrInvalidUpdate},
		{"stale", versionPointer(1), ErrTaskConflict},
		{"future", versionPointer(3), ErrTaskConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := NewMemoryRepository()
			service := NewService(repo, nil)
			task, err := service.Create(ctx, testOwner, CreateTaskRequest{Title: "Original", Priority: 2})
			if err != nil {
				t.Fatal(err)
			}
			title := "Latest"
			task, err = service.Update(ctx, testOwner, task.ID, UpdateTaskRequest{Title: &title, Version: versionPointer(1)})
			if err != nil {
				t.Fatal(err)
			}

			title = "Must not be saved"
			got, err := service.Update(ctx, testOwner, task.ID, UpdateTaskRequest{Title: &title, Version: tc.version})
			if !errors.Is(err, tc.want) || got != nil {
				t.Fatalf("Update() = %+v, %v; want nil, %v", got, err, tc.want)
			}
			saved, err := repo.FindByID(ctx, testOwner, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Title != "Latest" || saved.Version != 2 {
				t.Fatalf("rejected update changed task: %+v", saved)
			}
		})
	}
}

func versionPointer(v int64) *int64 { return &v }

// Both requests must read the same snapshot before either can write.
type synchronizedReadRepository struct {
	Repository
	readers sync.WaitGroup
}

func (r *synchronizedReadRepository) FindByID(ctx context.Context, ownerID session.UserID, id string) (*Task, error) {
	task, err := r.Repository.FindByID(ctx, ownerID, id)
	r.readers.Done()
	r.readers.Wait()
	return task, err
}

func TestServiceConcurrentUpdatesOnlyOneSucceeds(t *testing.T) {
	ctx := context.Background()
	memory := NewMemoryRepository()
	task, err := NewService(memory, nil).Create(ctx, testOwner, CreateTaskRequest{Title: "Original", Priority: 2})
	if err != nil {
		t.Fatal(err)
	}
	repo := &synchronizedReadRepository{Repository: memory}
	repo.readers.Add(2)
	service := NewService(repo, nil)
	type result struct {
		task *Task
		err  error
	}
	results := make(chan result, 2)
	for _, title := range []string{"First", "Second"} {
		go func(title string) {
			got, err := service.Update(ctx, testOwner, task.ID, UpdateTaskRequest{Title: &title, Version: versionPointer(1)})
			results <- result{got, err}
		}(title)
	}
	var winner *Task
	successes, conflicts := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result.task
		case errors.Is(result.err, ErrTaskConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1 each", successes, conflicts)
	}
	saved, err := memory.FindByID(ctx, testOwner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != winner.Title || saved.Version != 2 || winner.Version != 2 {
		t.Fatalf("saved=%+v winner=%+v", saved, winner)
	}
}

func TestHandlerUpdateVersionContract(t *testing.T) {
	router, repo := newTestRouter()
	ctx := context.Background()
	if err := repo.Save(ctx, testOwner, &Task{ID: "task-1", Title: "Original", Priority: 2}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body    string
		status  int
		title   string
		version int64
	}{
		{`{"title":"Missing version"}`, http.StatusBadRequest, "Original", 1},
		{`{"title":"Latest","version":1}`, http.StatusOK, "Latest", 2},
		{`{"title":"Stale","version":1}`, http.StatusConflict, "Latest", 2},
	} {
		req := httptest.NewRequest(http.MethodPatch, "/tasks/task-1", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		req.Header.Set("Authorization", "Bearer "+testToken)
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("body=%s: status=%d want=%d: %s", tc.body, rec.Code, tc.status, rec.Body.String())
		}
		saved, err := repo.FindByID(ctx, testOwner, "task-1")
		if err != nil {
			t.Fatal(err)
		}
		if saved.Title != tc.title || saved.Version != tc.version {
			t.Fatalf("unexpected saved task: %+v", saved)
		}
	}
}
