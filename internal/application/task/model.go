package applicationtask

import (
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

type CreateInput struct {
	ProjectID   *string
	Title       string
	Description string
	Priority    int
	DueDate     *time.Time
}

type UpdateInput = domaintask.Change

// OptionalTime distinguishes omitted, null, and concrete RFC3339 values.
type OptionalTime = domaintask.OptionalTime

// OptionalString represents missing, null (unassign), or a concrete project UUID.
type OptionalString = domaintask.OptionalString

type PageResult struct {
	Items      []TaskResult
	NextCursor string
}

// No project/owner/recurrence fields: strict JSON decoding rejects them.
type CreateTodayInput struct {
	PlanDate    string
	Title       string
	Description string
	Priority    int
	DueDate     *time.Time
}

type TodayResult struct {
	Items      []TaskResult
	NextCursor string
	PlanDate   string
	Timezone   string
	ServerTime time.Time
	NextDayAt  time.Time
}

type TaskResult struct {
	RecurrenceID *string
	PlanDate     *string
	CanMove      bool
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

func taskResult(task domaintask.Task, viewer domainaccount.UserID) TaskResult {
	return TaskResult{
		RecurrenceID: task.RecurrenceID,
		PlanDate:     task.PlanDate,
		CanMove:      task.OwnerID == viewer && task.PlanDate == nil,
		ProjectID:    task.ProjectID,
		OwnerID:      task.OwnerID,
		ID:           task.ID,
		Title:        task.Title,
		Description:  task.Description,
		Completed:    task.Completed,
		Priority:     task.Priority,
		CreatedAt:    task.CreatedAt,
		DueDate:      task.DueDate,
		Version:      task.Version,
	}
}
