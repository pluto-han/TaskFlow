package interfaceshttptask

import (
	"bytes"
	"encoding/json"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type CreateTaskRequest struct {
	ProjectID   *string    `json:"project_id"`
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	Priority    int        `json:"priority" binding:"required,min=1,max=3"`
	DueDate     *time.Time `json:"due_date"`
}

type UpdateTaskRequest struct {
	ProjectID   OptionalString `json:"project_id"`
	DueDate     OptionalTime   `json:"due_date"`
	Title       *string        `json:"title"`
	Description *string        `json:"description"`
	Priority    *int           `json:"priority"`
	Completed   *bool          `json:"completed"`
	Version     *int64         `json:"version" binding:"required,min=1"`
}

type CreateTodayTaskRequest struct {
	PlanDate    string     `json:"plan_date" binding:"required"`
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	Priority    int        `json:"priority" binding:"required,min=1,max=3"`
	DueDate     *time.Time `json:"due_date"`
}

type OptionalTime struct {
	Present bool
	Value   *time.Time
}

type OptionalString struct {
	Present bool
	Value   *string
}

type TodayPageResponse struct {
	Items      []TaskResponse `json:"items"`
	NextCursor string         `json:"next_cursor"`
	PlanDate   string         `json:"plan_date"`
	Timezone   string         `json:"timezone"`
	ServerTime time.Time      `json:"server_time"`
	NextDayAt  time.Time      `json:"next_day_at"`
}

type TaskResponse struct {
	RecurrenceID *string              `json:"recurrence_id"`
	PlanDate     *string              `json:"plan_date"`
	CanMove      bool                 `json:"can_move"`
	ProjectID    *string              `json:"project_id"`
	OwnerID      domainaccount.UserID `json:"-"`
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	Description  string               `json:"description"`
	Completed    bool                 `json:"completed"`
	Priority     int                  `json:"priority"`
	CreatedAt    time.Time            `json:"created_at"`
	DueDate      *time.Time           `json:"due_date"`
	Version      int64                `json:"version"`
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

func (o *OptionalString) UnmarshalJSON(raw []byte) error {
	o.Present = true
	o.Value = nil
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}

	o.Value = &value

	return nil
}
