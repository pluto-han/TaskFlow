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
