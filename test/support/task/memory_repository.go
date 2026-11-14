package task

import (
	"context"
	"sort"
	"sync"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

type MemoryRepository struct {
	mu    sync.RWMutex
	tasks map[string]domaintask.Task
}

// Construct function
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		tasks: make(map[string]domaintask.Task),
	}
}

func (r *MemoryRepository) FindByID(ctx context.Context, ownerID domainaccount.UserID, id string) (*domaintask.Task, error) {
	if ownerID == "" {
		return nil, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, exists := r.tasks[id]
	if !exists || task.OwnerID != ownerID {
		return nil, domaintask.ErrTaskNotFound
	}

	value := domaintask.CloneTask(task)
	return &value, nil
}

func (r *MemoryRepository) Save(ctx context.Context, ownerID domainaccount.UserID, task *domaintask.Task) error {
	if ownerID == "" {
		return domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.RecurrenceID != nil {
		return domaintask.ErrInvalidTask
	}
	if err := domaintask.ValidateTaskPlan(task); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[task.ID]; exists {
		return domaintask.ErrTaskConflict
	}
	task.OwnerID = ownerID
	task.Version = 1
	r.tasks[task.ID] = domaintask.CloneTask(*task)
	return nil
}

func (r *MemoryRepository) FindPage(ctx context.Context, ownerID domainaccount.UserID, query domaintask.PageQuery) ([]domaintask.Task, error) {
	if ownerID == "" {
		return nil, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := domaintask.ValidatePageQuery(query); err != nil {
		return nil, err
	}
	query.ListFilter, _ = domaintask.NormalizeFilter(query.ListFilter)
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]domaintask.Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if task.OwnerID != ownerID || !matchesFilter(task, query.ListFilter) {
			continue
		}
		if query.After != nil && (task.CreatedAt.After(query.After.CreatedAt) || (task.CreatedAt.Equal(query.After.CreatedAt) && task.ID >= query.After.ID)) {
			continue
		}
		tasks = append(tasks, domaintask.CloneTask(task))
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

func (r *MemoryRepository) Update(ctx context.Context, ownerID domainaccount.UserID, task *domaintask.Task) error {
	if ownerID == "" {
		return domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.tasks[task.ID]
	if !ok || existing.OwnerID != ownerID {
		return domaintask.ErrTaskNotFound
	}

	if existing.Version != task.Version {
		return domaintask.ErrTaskConflict
	}

	if err := domaintask.ValidateTaskPlan(task); err != nil {
		return err
	}
	if !domaintask.SameProject(existing.RecurrenceID, task.RecurrenceID) || !domaintask.SameProject(existing.PlanDate, task.PlanDate) {
		return domaintask.ErrInvalidUpdate
	}
	updated := *task
	updated.OwnerID = ownerID
	updated.Version++

	r.tasks[task.ID] = domaintask.CloneTask(updated)
	task.OwnerID = ownerID
	task.Version = updated.Version

	return nil
}

func (r *MemoryRepository) Delete(ctx context.Context, ownerID domainaccount.UserID, id string) error {
	if ownerID == "" {
		return domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if task, exists := r.tasks[id]; !exists || task.OwnerID != ownerID {
		return domaintask.ErrTaskNotFound
	}

	delete(r.tasks, id)
	return nil
}

func matchesFilter(task domaintask.Task, filter domaintask.ListFilter) bool {
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
