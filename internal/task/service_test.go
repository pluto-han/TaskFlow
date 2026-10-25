package task

import (
	"context"
	"errors"
	"testing"
)

// TestServiceCreate 测试创建任务的正常流程。
func TestServiceCreate(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)

	input := CreateTaskRequest{
		Title:       "Learn Go",
		Description: "Write unit tests",
		Priority:    2,
	}

	created, err := service.Create(context.Background(), input)
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

	saved, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("created task was not saved: %v", err)
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
			repo := NewMemoryRepository()
			service := NewService(repo)

			_, err := service.Create(
				context.Background(),
				CreateTaskRequest{
					Title:    tt.title,
					Priority: tt.priority,
				},
			)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidTask) {
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
	repo := NewMemoryRepository()
	service := NewService(repo)

	result, err := service.GetByID(
		context.Background(),
		"not-exist",
	)

	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

// TestServiceUpdate 测试部分更新，尤其是 completed=false。
func TestServiceUpdate(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)

	original := &Task{
		ID:        "task-1",
		Title:     "Learn Go",
		Priority:  2,
		Completed: true,
	}

	if err := repo.Save(original); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	completed := false

	updated, err := service.Update(
		context.Background(),
		"task-1",
		UpdateTaskRequest{
			Completed: &completed,
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

	saved, err := repo.FindByID("task-1")
	if err != nil {
		t.Fatalf("FindByID() error: %v", err)
	}

	if saved.Completed {
		t.Error("updated value was not persisted")
	}
}

// TestServiceUpdateValidation 测试非法 PATCH。
func TestServiceUpdateValidation(t *testing.T) {
	empty := ""
	invalidPriority := 5

	tests := []struct {
		name  string
		input UpdateTaskRequest
	}{
		{
			name:  "empty update",
			input: UpdateTaskRequest{},
		},
		{
			name: "empty title",
			input: UpdateTaskRequest{
				Title: &empty,
			},
		},
		{
			name: "invalid priority",
			input: UpdateTaskRequest{
				Priority: &invalidPriority,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewMemoryRepository()
			service := NewService(repo)

			err := repo.Save(&Task{
				ID:       "task-1",
				Title:    "Original",
				Priority: 2,
			})
			if err != nil {
				t.Fatalf("failed to seed task: %v", err)
			}

			_, err = service.Update(
				context.Background(),
				"task-1",
				tt.input,
			)

			if !errors.Is(err, ErrInvalidUpdate) {
				t.Errorf("expected ErrInvalidUpdate, got %v", err)
			}
		})
	}
}

// TestServiceDelete 测试删除任务。
func TestServiceDelete(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)

	if err := repo.Save(&Task{
		ID:       "task-1",
		Title:    "Delete me",
		Priority: 1,
	}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	err := service.Delete(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	_, err = service.GetByID(
		context.Background(),
		"task-1",
	)

	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound after deletion, got %v", err)
	}
}

// TestServiceDeleteNotFound 测试删除不存在的任务。
func TestServiceDeleteNotFound(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)

	err := service.Delete(context.Background(), "not-exist")

	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// failingRepository 用于模拟存储层故障。
// 只覆盖 Save；其他嵌入的方法不应在这个测试中调用。
type failingRepository struct {
	Repository
}

var errStorageUnavailable = errors.New("storage unavailable")

func (r *failingRepository) Save(task *Task) error {
	return errStorageUnavailable
}

// TestServiceCreateRepositoryError 验证底层错误是否正确传播。
func TestServiceCreateRepositoryError(t *testing.T) {
	repo := &failingRepository{}
	service := NewService(repo)

	_, err := service.Create(
		context.Background(),
		CreateTaskRequest{
			Title:    "Learn Go",
			Priority: 2,
		},
	)

	if !errors.Is(err, errStorageUnavailable) {
		t.Errorf("expected storage error, got %v", err)
	}
}
