package domaintask

import (
	"strings"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

func NormalizeFilter(f ListFilter) (ListFilter, error) {
	if f.PlanDate != nil {
		if _, err := domainplanning.ParseDate(*f.PlanDate); err != nil {
			return f, ErrInvalidList
		}
	}
	if f.ProjectID != nil && *f.ProjectID != "none" {
		id, err := domainproject.ParseID(*f.ProjectID)
		if err != nil {
			return f, ErrInvalidList
		}

		f.ProjectID = &id
	}

	if f.Priority != nil && (*f.Priority < 1 || *f.Priority > 3) {
		return f, ErrInvalidList
	}
	if f.DueFrom != nil {
		value := f.DueFrom.UTC().Truncate(time.Microsecond)
		f.DueFrom = &value
	}
	if f.DueBefore != nil {
		value := f.DueBefore.UTC().Truncate(time.Microsecond)
		f.DueBefore = &value
	}
	if f.DueFrom != nil && f.DueBefore != nil && !f.DueFrom.Before(*f.DueBefore) {
		return f, ErrInvalidList
	}
	if f.HasDueDate != nil && !*f.HasDueDate && (f.DueFrom != nil || f.DueBefore != nil) {
		return f, ErrInvalidList
	}

	for _, date := range []*time.Time{f.DueFrom, f.DueBefore} {
		if !ValidDate(date) {
			return f, ErrInvalidList
		}
	}

	return f, nil
}

func ValidDate(t *time.Time) bool { return t == nil || (t.UTC().Year() >= 1 && t.UTC().Year() <= 9999) }

func ValidatePageQuery(q PageQuery) error {
	if q.Limit < 1 || q.Limit > MaxLimit+1 {
		return ErrInvalidList
	}
	if _, err := NormalizeFilter(q.ListFilter); err != nil {
		return err
	}
	if q.After != nil && (q.After.CreatedAt.IsZero() || !ValidDate(&q.After.CreatedAt) || len(q.After.ID) == 0 || len(q.After.ID) > 128 || strings.ContainsRune(q.After.ID, 0)) {
		return ErrInvalidList
	}

	return nil
}

type Task struct {
	RecurrenceID *string
	PlanDate     *string
	ProjectID    *string
	OwnerID      domainaccount.UserID
	ID           string
	Title        string
	Description  string
	Completed    bool
	Priority     int
	CreatedAt    time.Time
	DueDate      *time.Time
	Version      int64
}

func CopyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	copy := value.UTC().Truncate(time.Microsecond)

	return &copy
}

func CloneTask(task Task) Task {
	task.DueDate = CopyTime(task.DueDate)
	if task.RecurrenceID != nil {
		value := *task.RecurrenceID
		task.RecurrenceID = &value
	}
	if task.PlanDate != nil {
		value := *task.PlanDate
		task.PlanDate = &value
	}
	if task.ProjectID != nil {
		value := *task.ProjectID
		task.ProjectID = &value
	}
	task.CreatedAt = task.CreatedAt.UTC().Truncate(time.Microsecond)

	return task
}

func ValidateTaskPlan(item *Task) error {
	if item.PlanDate == nil {
		return nil
	}
	if item.ProjectID != nil {
		return ErrInvalidTask
	}
	if _, err := domainplanning.ParseDate(*item.PlanDate); err != nil {
		return ErrInvalidTask
	}

	return nil
}

func SameProject(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

const MaxLimit = 100

type OptionalTime struct {
	Present bool
	Value   *time.Time
}

type OptionalString struct {
	Present bool
	Value   *string
}

type Change struct {
	ProjectID   OptionalString
	DueDate     OptionalTime
	Title       *string
	Description *string
	Priority    *int
	Completed   *bool
	Version     *int64
}

func (task *Task) Apply(input Change) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if task.Version != *input.Version {
		return ErrTaskConflict
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)

		if title == "" {
			return ErrInvalidUpdate
		}

		task.Title = title
	}

	if input.Description != nil {
		task.Description = *input.Description
	}

	if input.Priority != nil {
		if *input.Priority < 1 || *input.Priority > 3 {
			return ErrInvalidUpdate
		}

		task.Priority = *input.Priority
	}

	if input.Completed != nil {
		task.Completed = *input.Completed
	}

	if input.DueDate.Present {
		task.DueDate = CopyTime(input.DueDate.Value)
	}

	return nil
}

func NewTask(
	owner domainaccount.UserID,
	title, description string,
	priority int,
	due *time.Time,
) (*Task, error) {
	title = strings.TrimSpace(title)
	if title == "" || priority < 1 || priority > 3 || !ValidDate(due) {
		return nil, ErrInvalidTask
	}

	return &Task{
		OwnerID:     owner,
		Title:       title,
		Description: description,
		Priority:    priority,
		DueDate:     CopyTime(due),
		Version:     1,
	}, nil
}

func (input Change) Validate() error {
	if input.Version == nil || *input.Version < 1 {
		return ErrInvalidUpdate
	}
	if input.Title == nil && input.Description == nil && input.Priority == nil && input.Completed == nil && !input.DueDate.Present && !input.ProjectID.Present {
		return ErrInvalidUpdate
	}
	if input.DueDate.Present && !ValidDate(input.DueDate.Value) {
		return ErrInvalidUpdate
	}

	return nil
}
