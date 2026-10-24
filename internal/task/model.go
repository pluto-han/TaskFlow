package task

import "time"

type Task struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Completed   bool      `json:"completed"`
	Priority    uint      `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
}
