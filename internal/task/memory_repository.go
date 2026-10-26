package task

import (
	"context"
	"sync"

	"github.com/pluto-han/taskflow/internal/session"
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

func (r *MemoryRepository) FindByID(ctx context.Context, ownerID session.UserID, id string) (*Task, error) {
	if ownerID == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, exists := r.tasks[id]
	if !exists || task.OwnerID != ownerID {
		return nil, ErrTaskNotFound
	}

	return &task, nil
}

func (r *MemoryRepository) Save(ctx context.Context, ownerID session.UserID, task *Task) error {
	if ownerID == "" {
		return session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[task.ID]; exists {
		return ErrTaskConflict
	}
	task.OwnerID = ownerID
	task.Version = 1
	r.tasks[task.ID] = *task
	return nil
}

func (r *MemoryRepository) FindAll(ctx context.Context, ownerID session.UserID) ([]Task, error) {
	if ownerID == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if task.OwnerID != ownerID {
			continue
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (r *MemoryRepository) Update(ctx context.Context, ownerID session.UserID, task *Task) error {
	if ownerID == "" {
		return session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.tasks[task.ID]
	if !ok || existing.OwnerID != ownerID {
		return ErrTaskNotFound
	}

	if existing.Version != task.Version {
		return ErrTaskConflict
	}

	updated := *task
	updated.OwnerID = ownerID
	updated.Version++

	r.tasks[task.ID] = updated
	task.OwnerID = ownerID
	task.Version = updated.Version

	return nil
}

func (r *MemoryRepository) Delete(ctx context.Context, ownerID session.UserID, id string) error {
	if ownerID == "" {
		return session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if task, exists := r.tasks[id]; !exists || task.OwnerID != ownerID {
		return ErrTaskNotFound
	}

	delete(r.tasks, id)
	return nil
}
