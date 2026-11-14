package domaintask

import (
	"context"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type ListFilter struct {
	PlanDate   *string
	ProjectID  *string
	Completed  *bool
	Priority   *int
	HasDueDate *bool
	DueFrom    *time.Time
	DueBefore  *time.Time
}

type CursorPosition struct {
	CreatedAt time.Time
	ID        string
}

// PageQuery is the bounded repository contract. Limit includes the lookahead row.
type PageQuery struct {
	ListFilter
	Limit int
	After *CursorPosition
}

type Repository interface {
	FindByID(ctx context.Context, ownerID domainaccount.UserID, id string) (*Task, error)
	FindPage(ctx context.Context, ownerID domainaccount.UserID, query PageQuery) ([]Task, error)
	Save(ctx context.Context, ownerID domainaccount.UserID, task *Task) error
	Update(ctx context.Context, ownerID domainaccount.UserID, task *Task) error
	Delete(ctx context.Context, ownerID domainaccount.UserID, id string) error
}
