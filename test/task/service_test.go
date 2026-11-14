package task_test

import (
	"context"
	"errors"
	"testing"

	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	"github.com/pluto-han/taskflow/test/support/task"
)

// TestServiceCreate 测试创建任务的正常流程。
func TestServiceCreate(t *testing.T) {
	repo := task.NewMemoryRepository()
	service := applicationtask.New(repo, nil)

	input := applicationtask.CreateInput{
		Title:       "Learn Go",
		Description: "Write unit tests",
		Priority:    2,
	}

	created, err := service.Create(context.Background(), testOwner, input)
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	if created.ID == "" {
		t.Error("expected generated ID")
	}

	if created.Title != "Learn Go" {
		t.Errorf("expected title Learn Go, got %q", created.Title)
	}

	if created.Priority != 2 {
		t.Errorf("expected priority 2, got %d", created.Priority)
	}

	if created.Completed {
		t.Error("new task should not be completed")
	}

	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt should be initialized")
	}

	saved, err := repo.FindByID(context.Background(), testOwner, created.ID)
	if err != nil {
		t.Fatalf("created task was not saved: %v", err)
	}

	if created.Version != 1 || saved.Version != 1 {
		t.Fatalf("initial versions: created=%d saved=%d, want 1", created.Version, saved.Version)
	}

	if saved.Title != created.Title {
		t.Errorf("saved title = %q, want %q", saved.Title, created.Title)
	}
}

// TestServiceCreateValidation 使用表驱动测试验证输入。
func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		priority int
		wantErr  bool
	}{
		{"valid task", "Learn Go", 2, false},
		{"empty title", "", 2, true},
		{"whitespace title", "   ", 2, true},
		{"priority too low", "Learn Go", 0, true},
		{"priority too high", "Learn Go", 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := task.NewMemoryRepository()
			service := applicationtask.New(repo, nil)

			_, err := service.Create(
				context.Background(), testOwner,
				applicationtask.CreateInput{
					Title:    tt.title,
					Priority: tt.priority,
				},
			)

			if tt.wantErr {
				if !errors.Is(err, domaintask.ErrInvalidTask) {
					t.Errorf("expected ErrInvalidTask, got %v", err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestServiceGetByIDNotFound 测试查询不存在的任务。
func TestServiceGetByIDNotFound(t *testing.T) {
	repo := task.NewMemoryRepository()
	service := applicationtask.New(repo, nil)

	result, err := service.GetByID(
		context.Background(), testOwner,
		"not-exist",
	)

	if !errors.Is(err, domaintask.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

// TestServiceUpdate 测试部分更新，尤其是 completed=false。
func TestServiceUpdate(t *testing.T) {
	repo := task.NewMemoryRepository()
	service := applicationtask.New(repo, nil)

	original := &domaintask.Task{
		ID:        "task-1",
		Title:     "Learn Go",
		Priority:  2,
		Completed: true,
	}

	if err := repo.Save(context.Background(), testOwner, original); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	completed := false

	updated, err := service.Update(
		context.Background(), testOwner,
		"task-1",
		applicationtask.UpdateInput{
			Completed: &completed,
			Version:   &original.Version,
		},
	)

	if err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}

	if updated.Completed {
		t.Error("expected Completed=false")
	}

	if updated.Title != original.Title {
		t.Errorf("title changed unexpectedly: %q", updated.Title)
	}

	if updated.Priority != original.Priority {
		t.Errorf("priority changed unexpectedly: %d", updated.Priority)
	}

	saved, err := repo.FindByID(context.Background(), testOwner, "task-1")
	if err != nil {
		t.Fatalf("FindByID() error: %v", err)
	}

	if updated.Version != 2 || saved.Version != 2 {
		t.Fatalf("updated versions: response=%d saved=%d, want 2", updated.Version, saved.Version)
	}

	if saved.Completed {
		t.Error("updated value was not persisted")
	}
}

// TestServiceUpdateValidation 测试非法 PATCH。
func TestServiceUpdateValidation(t *testing.T) {
	empty := ""
	invalidPriority := 5
	version := int64(1)

	tests := []struct {
		name  string
		input applicationtask.UpdateInput
	}{
		{
			name:  "empty update",
			input: applicationtask.UpdateInput{},
		},
		{
			name: "empty title",
			input: applicationtask.UpdateInput{
				Title: &empty,
			},
		},
		{
			name: "invalid priority",
			input: applicationtask.UpdateInput{
				Priority: &invalidPriority,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := task.NewMemoryRepository()
			service := applicationtask.New(repo, nil)

			err := repo.Save(context.Background(), testOwner, &domaintask.Task{
				ID:       "task-1",
				Title:    "Original",
				Priority: 2,
			})
			if err != nil {
				t.Fatalf("failed to seed task: %v", err)
			}

			tt.input.Version = &version
			_, err = service.Update(
				context.Background(), testOwner,
				"task-1",
				tt.input,
			)

			if !errors.Is(err, domaintask.ErrInvalidUpdate) {
				t.Errorf("expected ErrInvalidUpdate, got %v", err)
			}
		})
	}
}

// TestServiceDelete 测试删除任务。
func TestServiceDelete(t *testing.T) {
	repo := task.NewMemoryRepository()
	service := applicationtask.New(repo, nil)

	if err := repo.Save(context.Background(), testOwner, &domaintask.Task{
		ID:       "task-1",
		Title:    "Delete me",
		Priority: 1,
	}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	err := service.Delete(context.Background(), testOwner, "task-1")
	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	_, err = service.GetByID(
		context.Background(), testOwner,
		"task-1",
	)

	if !errors.Is(err, domaintask.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound after deletion, got %v", err)
	}
}

// TestServiceDeleteNotFound 测试删除不存在的任务。
func TestServiceDeleteNotFound(t *testing.T) {
	repo := task.NewMemoryRepository()
	service := applicationtask.New(repo, nil)

	err := service.Delete(context.Background(), testOwner, "not-exist")

	if !errors.Is(err, domaintask.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// failingRepository 用于模拟存储层故障。
// 只覆盖 Save；其他嵌入的方法不应在这个测试中调用。
type failingRepository struct {
	domaintask.Repository
}

var errStorageUnavailable = errors.New("storage unavailable")

func (r *failingRepository) Save(ctx context.Context, ownerID domainaccount.UserID, task *domaintask.Task) error {
	return errStorageUnavailable
}

// TestServiceCreateRepositoryError 验证底层错误是否正确传播。
func TestServiceCreateRepositoryError(t *testing.T) {
	repo := &failingRepository{}
	service := applicationtask.New(repo, nil)

	_, err := service.Create(
		context.Background(), testOwner,
		applicationtask.CreateInput{
			Title:    "Learn Go",
			Priority: 2,
		},
	)

	if !errors.Is(err, errStorageUnavailable) {
		t.Errorf("expected storage error, got %v", err)
	}
}
