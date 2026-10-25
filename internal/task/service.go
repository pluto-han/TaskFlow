package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrInvalidTask = errors.New("invalid task")
var ErrInvalidUpdate = errors.New("invalid task update")

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, input CreateTaskRequest) (*Task, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	title := strings.TrimSpace(input.Title)

	if title == "" || input.Priority < 1 || input.Priority > 3 {
		return nil, ErrInvalidTask
	}

	idBytes := make([]byte, 16)

	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}

	newTask := &Task{
		ID:          hex.EncodeToString(idBytes),
		Title:       title,
		Description: input.Description,
		Priority:    input.Priority,
		Completed:   false,
		CreatedAt:   time.Now().UTC(),
		DueDate:     input.DueDate,
		Version:     1,
	}

	if err := s.repo.Save(ctx, newTask); err != nil {
		return nil, err
	}

	return newTask, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindAll(ctx)
}

func (s *Service) Update(ctx context.Context, id string, input UpdateTaskRequest) (*Task, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if input.Version == nil || *input.Version < 1 {
		return nil, ErrInvalidUpdate
	}

	if input.Title == nil &&
		input.Description == nil &&
		input.Priority == nil &&
		input.Completed == nil {
		return nil, ErrInvalidUpdate
	}

	current, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if current.Version != *input.Version {
		return nil, ErrTaskConflict
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)

		if title == "" {
			return nil, ErrInvalidUpdate
		}

		current.Title = title
	}

	if input.Description != nil {
		current.Description = *input.Description
	}

	if input.Priority != nil {
		if *input.Priority < 1 || *input.Priority > 3 {
			return nil, ErrInvalidUpdate
		}

		current.Priority = *input.Priority
	}

	if input.Completed != nil {
		current.Completed = *input.Completed
	}

	if err := s.repo.Update(ctx, current); err != nil {
		return nil, err
	}

	return current, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	return s.repo.Delete(ctx, id)
}
