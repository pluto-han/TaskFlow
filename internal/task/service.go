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

type Service struct {
	repo Repoisitory
}

func NewService(repo Repoisitory) *Service {
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
	}

	if err := s.repo.Save(newTask); err != nil {
		return nil, err
	}

	return newTask, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindByID(id)
}

func (s *Service) List(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindAll()
}
