package task

import (
	"time"

	"github.com/pluto-han/taskflow/internal/session"
)

type Task struct {
	OwnerID     session.UserID `json:"-"`
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Completed   bool           `json:"completed"`
	Priority    int            `json:"priority"`
	CreatedAt   time.Time      `json:"created_at"`
	DueDate     *time.Time     `json:"due_date"`
	Version     int64          `json:"version"`
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
	task.CreatedAt = task.CreatedAt.UTC().Truncate(time.Microsecond)
	return task
}
