package task

import (
	"context"
	"errors"
)

var ErrTaskNotFound = errors.New("task not found")

type Repository interface {
	FindByID(ctx context.Context, id string) (*Task, error)
	FindAll(ctx context.Context) ([]Task, error)
	Save(ctx context.Context, task *Task) error
	Update(ctx context.Context, task *Task) error
	Delete(ctx context.Context, id string) error
}
