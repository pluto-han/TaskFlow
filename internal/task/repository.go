package task

import (
	"context"
	"errors"

	"github.com/pluto-han/taskflow/internal/session"
)

var ErrTaskNotFound = errors.New("task not found")

type Repository interface {
	FindByID(ctx context.Context, ownerID session.UserID, id string) (*Task, error)
	FindPage(ctx context.Context, ownerID session.UserID, query PageQuery) ([]Task, error)
	Save(ctx context.Context, ownerID session.UserID, task *Task) error
	Update(ctx context.Context, ownerID session.UserID, task *Task) error
	Delete(ctx context.Context, ownerID session.UserID, id string) error
}
