package recurrence

import (
	"context"
	"errors"
	"time"

	"github.com/pluto-han/taskflow/internal/session"
)

var (
	ErrInvalid  = errors.New("invalid recurrence")
	ErrNotFound = errors.New("recurrence not found")
	ErrConflict = errors.New("recurrence version conflict")
)

type Rule struct {
	ReminderEnabled bool           `json:"reminder_enabled"`
	ID              string         `json:"id"`
	OwnerID         session.UserID `json:"-"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Priority        int            `json:"priority"`
	AnchorDate      string         `json:"anchor_date"`
	IntervalDays    int            `json:"interval_days"`
	NextRunDate     string         `json:"next_run_date"`
	Paused          bool           `json:"paused"`
	Version         int64          `json:"version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       *time.Time     `json:"-"`
	PreviewDates    []string       `json:"preview_dates"`
}
type CreateRequest struct {
	ReminderEnabled bool   `json:"reminder_enabled"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Priority        int    `json:"priority"`
	AnchorDate      string `json:"anchor_date"`
	IntervalDays    int    `json:"interval_days"`
}
type UpdateRequest struct {
	ReminderEnabled *bool   `json:"reminder_enabled"`
	Version         int64   `json:"version"`
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	Priority        *int    `json:"priority"`
	AnchorDate      *string `json:"anchor_date"`
	IntervalDays    *int    `json:"interval_days"`
	Paused          *bool   `json:"paused"`
}
type DeleteRequest struct {
	Version int64 `json:"version"`
}
type PreviewRequest struct {
	AnchorDate   string `json:"anchor_date"`
	IntervalDays int    `json:"interval_days"`
}
type Preview struct {
	PlanDate string   `json:"plan_date"`
	Timezone string   `json:"timezone"`
	Dates    []string `json:"dates"`
}
type Page struct {
	Items            []Rule `json:"items"`
	NextCursor       string `json:"next_cursor"`
	PlanDate         string `json:"plan_date"`
	Timezone         string `json:"timezone"`
	SchedulerEnabled bool   `json:"scheduler_enabled"`
}

// Change executes under the same row lock future schedulers must acquire.
// lastDate is the last receipt, so resuming never recreates an already-run day.
type Repository interface {
	Create(context.Context, *Rule) (*Rule, error)
	Get(context.Context, session.UserID, string) (*Rule, error)
	List(context.Context, session.UserID, string, int) ([]Rule, error)
	Change(context.Context, session.UserID, string, int64, func(*Rule, string) error) (*Rule, error)
}
