package task

import (
	"bytes"
	"encoding/json"
	"time"
)

type CreateTaskRequest struct {
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	Priority    int        `json:"priority" binding:"required,min=1,max=3"`
	DueDate     *time.Time `json:"due_date"`
}

type UpdateTaskRequest struct {
	DueDate     OptionalTime `json:"due_date"`
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Priority    *int         `json:"priority"`
	Completed   *bool        `json:"completed"`
	Version     *int64       `json:"version" binding:"required,min=1"`
}

// OptionalTime distinguishes omitted, null, and concrete RFC3339 values.
type OptionalTime struct {
	Present bool
	Value   *time.Time
}

func (o *OptionalTime) UnmarshalJSON(raw []byte) error {
	o.Present = true
	o.Value = nil
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value time.Time
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	value = value.UTC()
	o.Value = &value
	return nil
}
