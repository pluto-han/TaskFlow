package task

import (
	"context"
	"sort"
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

	value := cloneTask(task)
	return &value, nil
}

func (r *MemoryRepository) Save(ctx context.Context, ownerID session.UserID, task *Task) error {
	if ownerID == "" {
		return session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateTaskPlan(task); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[task.ID]; exists {
		return ErrTaskConflict
	}
	task.OwnerID = ownerID
	task.Version = 1
	r.tasks[task.ID] = cloneTask(*task)
	return nil
}

func (r *MemoryRepository) FindPage(ctx context.Context, ownerID session.UserID, query PageQuery) ([]Task, error) {
	if ownerID == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePageQuery(query); err != nil {
		return nil, err
	}
	query.ListFilter, _ = normalizeFilter(query.ListFilter)
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if task.OwnerID != ownerID || !matchesFilter(task, query.ListFilter) {
			continue
		}
		if query.After != nil && (task.CreatedAt.After(query.After.CreatedAt) || (task.CreatedAt.Equal(query.After.CreatedAt) && task.ID >= query.After.ID)) {
			continue
		}
		tasks = append(tasks, cloneTask(task))
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID > tasks[j].ID
		}
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	if len(tasks) > query.Limit {
		tasks = tasks[:query.Limit]
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

	if err := validateTaskPlan(task); err != nil {
		return err
	}
	if !sameProject(existing.PlanDate, task.PlanDate) {
		return ErrInvalidUpdate
	}
	updated := *task
	updated.OwnerID = ownerID
	updated.Version++

	r.tasks[task.ID] = cloneTask(updated)
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

func matchesFilter(task Task, filter ListFilter) bool {
	if filter.PlanDate != nil && (task.PlanDate == nil || *task.PlanDate != *filter.PlanDate) {
		return false
	}
	if filter.ProjectID != nil {
		if *filter.ProjectID == "none" {
			if task.ProjectID != nil {
				return false
			}
		} else if task.ProjectID == nil || *task.ProjectID != *filter.ProjectID {
			return false
		}
	}

	if filter.Completed != nil && task.Completed != *filter.Completed {
		return false
	}
	if filter.Priority != nil && task.Priority != *filter.Priority {
		return false
	}
	if filter.HasDueDate != nil && (task.DueDate != nil) != *filter.HasDueDate {
		return false
	}
	if filter.DueFrom != nil && (task.DueDate == nil || task.DueDate.Before(*filter.DueFrom)) {
		return false
	}
	if filter.DueBefore != nil && (task.DueDate == nil || !task.DueDate.Before(*filter.DueBefore)) {
		return false
	}
	return true
}
