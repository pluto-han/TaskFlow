package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/session"
)

var ErrInvalidTask = errors.New("invalid task")
var ErrInvalidUpdate = errors.New("invalid task update")

type ProjectReader interface {
	GetByID(context.Context, session.UserID, string) (*project.Project, error)
}

type Service struct {
	now      func() time.Time
	projects ProjectReader
	repo     Repository
}

func NewService(repo Repository, projects ProjectReader) *Service {
	return &Service{repo: repo, projects: projects, now: time.Now}
}

func (s *Service) Create(ctx context.Context, ownerID session.UserID, input CreateTaskRequest) (*Task, error) {
	return s.create(ctx, ownerID, input, nil, s.now())
}

func (s *Service) create(ctx context.Context, ownerID session.UserID, input CreateTaskRequest, planDate *string, now time.Time) (*Task, error) {

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

	projectID, err := s.checkProject(ctx, ownerID, input.ProjectID)
	if err != nil {
		return nil, err
	}

	idBytes := make([]byte, 16)

	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}

	newTask := &Task{
		CanMove:     planDate == nil,
		PlanDate:    planDate,
		ProjectID:   projectID,
		OwnerID:     ownerID,
		ID:          hex.EncodeToString(idBytes),
		Title:       title,
		Description: input.Description,
		Priority:    input.Priority,
		Completed:   false,
		CreatedAt:   now.UTC().Truncate(time.Microsecond),
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

	result, err := s.repo.FindByID(ctx, ownerID, id)
	if err == nil {
		result.CanMove = result.OwnerID == ownerID && result.PlanDate == nil
	}
	return result, err
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
	for i := range rows {
		rows[i].CanMove = rows[i].OwnerID == ownerID && rows[i].PlanDate == nil
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
		input.Completed == nil && !input.DueDate.Present && !input.ProjectID.Present {
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

	if input.ProjectID.Present {
		if current.PlanDate != nil && input.ProjectID.Value != nil {
			return nil, ErrInvalidUpdate
		}
		target, e := s.checkProject(ctx, ownerID, input.ProjectID.Value)
		if e != nil {
			return nil, e
		}
		if !sameProject(current.ProjectID, target) && current.OwnerID != ownerID {
			return nil, project.ErrForbidden
		}
		current.ProjectID = target
		if err != nil {
			return nil, err
		}
	}

	if err := s.repo.Update(ctx, ownerID, current); err != nil {
		return nil, err
	}

	current.CanMove = current.OwnerID == ownerID && current.PlanDate == nil
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

func (s *Service) checkProject(ctx context.Context, owner session.UserID, raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	id, err := project.ParseID(*raw)
	if err != nil {
		return nil, project.ErrInvalid
	}
	if s.projects == nil {
		return nil, project.ErrNotFound
	}
	if _, err := s.projects.GetByID(ctx, owner, id); err != nil {
		return nil, err
	}
	return &id, nil
}
