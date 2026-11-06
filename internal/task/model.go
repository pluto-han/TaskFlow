package task

import (
	"time"

	"github.com/pluto-han/taskflow/internal/session"
)

type Task struct {
	RecurrenceID *string        `json:"recurrence_id"`
	PlanDate     *string        `json:"plan_date"`
	CanMove      bool           `json:"can_move"`
	ProjectID    *string        `json:"project_id"`
	OwnerID      session.UserID `json:"-"`
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Completed    bool           `json:"completed"`
	Priority     int            `json:"priority"`
	CreatedAt    time.Time      `json:"created_at"`
	DueDate      *time.Time     `json:"due_date"`
	Version      int64          `json:"version"`
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC().Truncate(time.Microsecond)
	return &copy
}

func cloneTask(task Task) Task {
	task.DueDate = copyTime(task.DueDate)
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
