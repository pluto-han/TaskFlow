package task

import "time"

type CreateTaskRequest struct {
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	Priority    int        `json:"priority" binding:"required,min=1,max=3"`
	DueDate     *time.Time `json:"due_date"`
}

type UpdateTaskRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Priority    *int    `json:"priority"`
	Completed   *bool   `json:"completed"`
	Version     *int64  `json:"version" binding:"required,min=1"`
}
