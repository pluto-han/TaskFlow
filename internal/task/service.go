package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/pluto-han/taskflow/internal/session"
)

var ErrInvalidTask = errors.New("invalid task")
var ErrInvalidUpdate = errors.New("invalid task update")

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, ownerID session.UserID, input CreateTaskRequest) (*Task, error) {

	if ownerID == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	title := strings.TrimSpace(input.Title)

	if title == "" || input.Priority < 1 || input.Priority > 3 || !validDate(input.DueDate) {
		return nil, ErrInvalidTask
	}

	idBytes := make([]byte, 16)

	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}

	newTask := &Task{
		OwnerID:     ownerID,
		ID:          hex.EncodeToString(idBytes),
		Title:       title,
		Description: input.Description,
		Priority:    input.Priority,
		Completed:   false,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		DueDate:     copyTime(input.DueDate),
		Version:     1,
	}

	if err := s.repo.Save(ctx, ownerID, newTask); err != nil {
		return nil, err
	}

	return newTask, nil
}

func (s *Service) GetByID(ctx context.Context, ownerID session.UserID, id string) (*Task, error) {
	if ownerID == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindByID(ctx, ownerID, id)
}

func (s *Service) List(ctx context.Context, ownerID session.UserID, options ListOptions) (TaskPage, error) {
	if ownerID == "" {
		return TaskPage{}, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return TaskPage{}, err
	}
	if options.Limit == 0 {
		options.Limit = DefaultLimit
	}
	if options.Limit < 1 || options.Limit > MaxLimit {
		return TaskPage{}, ErrInvalidList
	}
	filter, err := normalizeFilter(options.ListFilter)
	if err != nil {
		return TaskPage{}, err
	}
	scope, err := cursorScope(ownerID, filter)
	if err != nil {
		return TaskPage{}, err
	}
	query := PageQuery{ListFilter: filter, Limit: options.Limit + 1}
	if options.Cursor != "" {
		query.After, err = decodeCursor(options.Cursor, scope)
		if err != nil {
			return TaskPage{}, err
		}
	}
	rows, err := s.repo.FindPage(ctx, ownerID, query)
	if err != nil {
		return TaskPage{}, err
	}
	page := TaskPage{Items: rows}
	if page.Items == nil {
		page.Items = []Task{}
	}
	if len(rows) > options.Limit {
		page.Items = rows[:options.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeCursor(CursorPosition{CreatedAt: last.CreatedAt, ID: last.ID}, scope)
		if err != nil {
			return TaskPage{}, err
		}
	}
	return page, nil
}

func (s *Service) Update(ctx context.Context, ownerID session.UserID, id string, input UpdateTaskRequest) (*Task, error) {

	if ownerID == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if input.Version == nil || *input.Version < 1 {
		return nil, ErrInvalidUpdate
	}

	if input.Title == nil &&
		input.Description == nil &&
		input.Priority == nil &&
		input.Completed == nil && !input.DueDate.Present {
		return nil, ErrInvalidUpdate
	}

	if input.DueDate.Present && !validDate(input.DueDate.Value) {
		return nil, ErrInvalidUpdate
	}

	current, err := s.repo.FindByID(ctx, ownerID, id)
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

	if input.DueDate.Present {
		current.DueDate = copyTime(input.DueDate.Value)
	}

	if err := s.repo.Update(ctx, ownerID, current); err != nil {
		return nil, err
	}

	return current, nil
}

func (s *Service) Delete(ctx context.Context, ownerID session.UserID, id string) error {

	if ownerID == "" {
		return session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.repo.Delete(ctx, ownerID, id)
}
