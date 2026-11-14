package task_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	"github.com/pluto-han/taskflow/test/support/task"
)

func TestToDomainTaskPreservesVersion(t *testing.T) {
	ctx, pool := taskDatabase(t)
	const owner = "00000000-0000-4000-8000-000000000707"
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES($1,'version-mapping@example.com','fixture')", owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO tasks(id,owner_id,title,priority,version) VALUES('version-mapping',$1,'mapping',1,7)", owner); err != nil {
		t.Fatal(err)
	}
	got, err := infratask.New(pool).FindByID(ctx, domainaccount.UserID(owner), "version-mapping")
	if err != nil || got.Version != 7 {
		t.Fatalf("version mapping = %+v, %v", got, err)
	}

}

func TestServiceUpdateRejectedVersionDoesNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version *int64
		want    error
	}{
		{"missing", nil, domaintask.ErrInvalidUpdate},
		{"zero", versionPointer(0), domaintask.ErrInvalidUpdate},
		{"negative", versionPointer(-1), domaintask.ErrInvalidUpdate},
		{"stale", versionPointer(1), domaintask.ErrTaskConflict},
		{"future", versionPointer(3), domaintask.ErrTaskConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := task.NewMemoryRepository()
			service := applicationtask.New(repo, nil)
			task, err := service.Create(ctx, testOwner, applicationtask.CreateInput{Title: "Original", Priority: 2})
			if err != nil {
				t.Fatal(err)
			}
			title := "Latest"
			task, err = service.Update(ctx, testOwner, task.ID, applicationtask.UpdateInput{Title: &title, Version: versionPointer(1)})
			if err != nil {
				t.Fatal(err)
			}

			title = "Must not be saved"
			got, err := service.Update(ctx, testOwner, task.ID, applicationtask.UpdateInput{Title: &title, Version: tc.version})
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
	domaintask.Repository
	readers sync.WaitGroup
}

func (r *synchronizedReadRepository) FindByID(ctx context.Context, ownerID domainaccount.UserID, id string) (*domaintask.Task, error) {
	task, err := r.Repository.FindByID(ctx, ownerID, id)
	r.readers.Done()
	r.readers.Wait()
	return task, err
}

func TestServiceConcurrentUpdatesOnlyOneSucceeds(t *testing.T) {
	ctx := context.Background()
	memory := task.NewMemoryRepository()
	task, err := applicationtask.New(memory, nil).Create(ctx, testOwner, applicationtask.CreateInput{Title: "Original", Priority: 2})
	if err != nil {
		t.Fatal(err)
	}
	repo := &synchronizedReadRepository{Repository: memory}
	repo.readers.Add(2)
	service := applicationtask.New(repo, nil)
	type result struct {
		task *applicationtask.TaskResult
		err  error
	}
	results := make(chan result, 2)
	for _, title := range []string{"First", "Second"} {
		go func(title string) {
			got, err := service.Update(ctx, testOwner, task.ID, applicationtask.UpdateInput{Title: &title, Version: versionPointer(1)})
			results <- result{got, err}
		}(title)
	}
	var winner *applicationtask.TaskResult
	successes, conflicts := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result.task
		case errors.Is(result.err, domaintask.ErrTaskConflict):
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
	if err := repo.Save(ctx, testOwner, &domaintask.Task{ID: "task-1", Title: "Original", Priority: 2}); err != nil {
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
