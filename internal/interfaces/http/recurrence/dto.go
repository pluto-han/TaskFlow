package interfaceshttprecurrence

import (
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

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

type PreviewResponse struct {
	PlanDate string   `json:"plan_date"`
	Timezone string   `json:"timezone"`
	Dates    []string `json:"dates"`
}

type PageResponse struct {
	Items            []RuleResponse `json:"items"`
	NextCursor       string         `json:"next_cursor"`
	PlanDate         string         `json:"plan_date"`
	Timezone         string         `json:"timezone"`
	SchedulerEnabled bool           `json:"scheduler_enabled"`
}

type RuleResponse struct {
	ReminderEnabled bool                 `json:"reminder_enabled"`
	ID              string               `json:"id"`
	OwnerID         domainaccount.UserID `json:"-"`
	Title           string               `json:"title"`
	Description     string               `json:"description"`
	Priority        int                  `json:"priority"`
	AnchorDate      string               `json:"anchor_date"`
	IntervalDays    int                  `json:"interval_days"`
	NextRunDate     string               `json:"next_run_date"`
	Paused          bool                 `json:"paused"`
	Version         int64                `json:"version"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	DeletedAt       *time.Time           `json:"-"`
	PreviewDates    []string             `json:"preview_dates"`
}
