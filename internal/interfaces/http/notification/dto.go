package interfaceshttpnotification

import (
	"time"
)

type EventResponse struct {
	ID            string    `json:"id"`
	RuleID        string    `json:"rule_id"`
	TaskID        string    `json:"task_id"`
	ScheduledDate string    `json:"scheduled_date"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error"`
	CreatedAt     time.Time `json:"created_at"`
}
