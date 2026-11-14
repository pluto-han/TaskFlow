package applicationrecurrence

import (
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
)

type CreateInput struct {
	ReminderEnabled bool
	Title           string
	Description     string
	Priority        int
	AnchorDate      string
	IntervalDays    int
}

type UpdateInput = domainrecurrence.Change

type PreviewInput struct {
	AnchorDate   string
	IntervalDays int
}

type PreviewResult struct {
	PlanDate string
	Timezone string
	Dates    []string
}

type PageResult struct {
	Items            []RuleResult
	NextCursor       string
	PlanDate         string
	Timezone         string
	SchedulerEnabled bool
}

type cursor struct {
	Owner domainaccount.UserID `json:"owner"`
	ID    string               `json:"id"`
}

type RuleResult struct {
	ReminderEnabled bool
	ID              string
	OwnerID         domainaccount.UserID
	Title           string
	Description     string
	Priority        int
	AnchorDate      string
	IntervalDays    int
	NextRunDate     string
	Paused          bool
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	PreviewDates    []string
}
