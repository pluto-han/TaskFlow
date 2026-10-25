package task

import (
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

func (r *MemoryRepository) FindByID(id string) (*Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, exists := r.tasks[id]
	if !exists {
		return nil, ErrTaskNotFound
	}

	return &task, nil
}

func (r *MemoryRepository) Save(task *Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tasks[task.ID] = *task
	return nil
}

func (r *MemoryRepository) FindAll() ([]Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}

	return tasks, nil
}
