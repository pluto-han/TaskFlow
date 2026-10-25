package task

import (
	"context"
	"sync"
)

type MemoryRepository struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

// Construct function
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		tasks: make(map[string]Task),
	}
}

func (r *MemoryRepository) FindByID(ctx context.Context, id string) (*Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, exists := r.tasks[id]
	if !exists {
		return nil, ErrTaskNotFound
	}

	return &task, nil
}

func (r *MemoryRepository) Save(ctx context.Context, task *Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	task.Version = 1
	r.tasks[task.ID] = *task
	return nil
}

func (r *MemoryRepository) FindAll(ctx context.Context) ([]Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (r *MemoryRepository) Update(ctx context.Context, task *Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.tasks[task.ID]
	if !ok {
		return ErrTaskNotFound
	}

	if existing.Version != task.Version {
		return ErrTaskConflict
	}

	updated := *task
	updated.Version++

	r.tasks[task.ID] = updated
	task.Version = updated.Version

	return nil
}

func (r *MemoryRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[id]; !exists {
		return ErrTaskNotFound
	}

	delete(r.tasks, id)
	return nil
}
