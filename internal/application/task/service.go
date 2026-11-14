package applicationtask

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

type ProjectReader interface {
	GetByID(context.Context, domainaccount.UserID, string) (*domainproject.Project, error)
}

type Service struct {
	todayCache    Store
	cacheObserver TodayCacheObserver
	now           func() time.Time
	projects      ProjectReader
	repo          domaintask.Repository
}

func New(repo domaintask.Repository, projects ProjectReader) *Service {
	return &Service{repo: repo, projects: projects, now: time.Now}
}

func (s *Service) Create(
	ctx context.Context,
	ownerID domainaccount.UserID,
	input CreateInput,
) (*TaskResult, error) {
	return s.create(ctx, ownerID, input, nil, s.now())
}

func (s *Service) create(
	ctx context.Context,
	ownerID domainaccount.UserID,
	input CreateInput,
	planDate *string,
	now time.Time,
) (*TaskResult, error) {

	if ownerID == "" {
		return nil, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	newTask, err := domaintask.NewTask(ownerID, input.Title, input.Description, input.Priority, input.DueDate)
	if err != nil {
		return nil, err
	}

	projectID, err := s.checkProject(ctx, ownerID, input.ProjectID)
	if err != nil {
		return nil, err
	}

	idBytes := make([]byte, 16)

	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}

	newTask.ID = hex.EncodeToString(idBytes)
	newTask.PlanDate = planDate
	newTask.ProjectID = projectID
	newTask.CreatedAt = now.UTC().Truncate(time.Microsecond)

	if err := s.repo.Save(ctx, ownerID, newTask); err != nil {
		return nil, err
	}

	s.invalidateTodayAfterWrite(ctx, ownerID)

	result := taskResult(*newTask, ownerID)

	return &result, nil
}

func (s *Service) GetByID(
	ctx context.Context,
	ownerID domainaccount.UserID,
	id string,
) (*TaskResult, error) {
	if ownerID == "" {
		return nil, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result, err := s.repo.FindByID(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}

	out := taskResult(*result, ownerID)

	return &out, nil
}

func (s *Service) List(
	ctx context.Context,
	ownerID domainaccount.UserID,
	options ListOptions,
) (PageResult, error) {
	if ownerID == "" {
		return PageResult{}, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return PageResult{}, err
	}
	if options.Limit == 0 {
		options.Limit = DefaultLimit
	}
	if options.Limit < 1 || options.Limit > MaxLimit {
		return PageResult{}, domaintask.ErrInvalidList
	}

	filter, err := domaintask.NormalizeFilter(options.ListFilter)
	if err != nil {
		return PageResult{}, err
	}

	scope, err := cursorScope(ownerID, filter)
	if err != nil {
		return PageResult{}, err
	}

	query := domaintask.PageQuery{ListFilter: filter, Limit: options.Limit + 1}
	if options.Cursor != "" {
		query.After, err = decodeCursor(options.Cursor, scope)
		if err != nil {
			return PageResult{}, err
		}
	}
	rows, err := s.repo.FindPage(ctx, ownerID, query)
	if err != nil {
		return PageResult{}, err
	}

	items := make([]TaskResult, len(rows))
	for i := range rows {
		items[i] = taskResult(rows[i], ownerID)
	}
	page := PageResult{Items: items}
	if page.Items == nil {
		page.Items = []TaskResult{}
	}
	if len(rows) > options.Limit {
		page.Items = items[:options.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeCursor(domaintask.CursorPosition{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}, scope)
		if err != nil {
			return PageResult{}, err
		}
	}
	return page, nil
}

func (s *Service) Update(
	ctx context.Context,
	ownerID domainaccount.UserID,
	id string,
	input UpdateInput,
) (*TaskResult, error) {

	if ownerID == "" {
		return nil, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := domaintask.Change(input).Validate(); err != nil {
		return nil, err
	}

	current, err := s.repo.FindByID(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}

	if err := current.Apply(domaintask.Change(input)); err != nil {
		return nil, err
	}

	if input.ProjectID.Present {
		if current.PlanDate != nil && input.ProjectID.Value != nil {
			return nil, domaintask.ErrInvalidUpdate
		}

		target, e := s.checkProject(ctx, ownerID, input.ProjectID.Value)
		if e != nil {
			return nil, e
		}
		if !domaintask.SameProject(current.ProjectID, target) && current.OwnerID != ownerID {
			return nil, domainproject.ErrForbidden
		}

		current.ProjectID = target
	}

	if err := s.repo.Update(ctx, ownerID, current); err != nil {
		return nil, err
	}

	s.invalidateTodayAfterWrite(ctx, ownerID)

	result := taskResult(*current, ownerID)

	return &result, nil
}

func (s *Service) Delete(ctx context.Context, ownerID domainaccount.UserID, id string) error {

	if ownerID == "" {
		return domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, ownerID, id); err != nil {
		return err
	}

	s.invalidateTodayAfterWrite(ctx, ownerID)

	return nil
}

func (s *Service) checkProject(
	ctx context.Context,
	owner domainaccount.UserID,
	raw *string,
) (*string, error) {
	if raw == nil {
		return nil, nil
	}

	id, err := domainproject.ParseID(*raw)
	if err != nil {
		return nil, domainproject.ErrInvalid
	}
	if s.projects == nil {
		return nil, domainproject.ErrNotFound
	}
	if _, err := s.projects.GetByID(ctx, owner, id); err != nil {
		return nil, err
	}

	return &id, nil
}

// NewWithClock permits deterministic calendar tests, without changing host time.
func NewWithClock(
	repo domaintask.Repository,
	projects ProjectReader,
	now func() time.Time,
) *Service {
	s := New(repo, projects)
	if now != nil {
		s.now = now
	}
	return s
}
