package task

import (
	"context"
	"errors"
	"time"

	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/session"
)

var ErrPlanDayChanged = errors.New("plan day changed")

// No project/owner/recurrence fields: strict JSON decoding rejects them.
type CreateTodayTaskRequest struct {
	PlanDate    string     `json:"plan_date" binding:"required"`
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	Priority    int        `json:"priority" binding:"required,min=1,max=3"`
	DueDate     *time.Time `json:"due_date"`
}

type TodayPage struct {
	Items      []Task    `json:"items"`
	NextCursor string    `json:"next_cursor"`
	PlanDate   string    `json:"plan_date"`
	Timezone   string    `json:"timezone"`
	ServerTime time.Time `json:"server_time"`
	NextDayAt  time.Time `json:"next_day_at"`
}

// NewServiceWithClock permits deterministic calendar tests, without changing host time.
func NewServiceWithClock(repo Repository, projects ProjectReader, now func() time.Time) *Service {
	s := NewService(repo, projects)
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) CreateToday(ctx context.Context, owner session.UserID, input CreateTodayTaskRequest) (*Task, error) {
	if owner == "" {
		return nil, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := planning.ParseDate(input.PlanDate); err != nil {
		return nil, ErrInvalidTask
	}
	// One captured acceptance instant determines both plan_date and created_at.
	now := s.now()
	day := planning.At(now)
	if input.PlanDate != day.PlanDate {
		return nil, ErrPlanDayChanged
	}
	return s.create(ctx, owner, CreateTaskRequest{Title: input.Title, Description: input.Description,
		Priority: input.Priority, DueDate: input.DueDate}, &day.PlanDate, now)
}

func (s *Service) ListToday(ctx context.Context, owner session.UserID, options ListOptions) (TodayPage, error) {
	if owner == "" {
		return TodayPage{}, session.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return TodayPage{}, err
	}
	day := planning.At(s.now())
	// The today endpoint cannot select projects or historical dates.
	if options.PlanDate != nil || options.ProjectID != nil {
		return TodayPage{}, ErrInvalidList
	}
	var page TaskPage
	var err error
	if s.todayCache != nil && todayCacheEligible(options) {
		page, day, err = s.listTodayCached(ctx, owner, day)
	} else {
		s.observeCacheRead("bypass")
		options.PlanDate = &day.PlanDate
		page, err = s.List(ctx, owner, options)
	}
	if err != nil {
		return TodayPage{}, err
	}
	return TodayPage{Items: page.Items, NextCursor: page.NextCursor, PlanDate: day.PlanDate,
		Timezone: day.Timezone, ServerTime: day.ServerTime, NextDayAt: day.NextDayAt}, nil
}

func validateTaskPlan(item *Task) error {
	if item.PlanDate == nil {
		return nil
	}
	if item.ProjectID != nil {
		return ErrInvalidTask
	}
	if _, err := planning.ParseDate(*item.PlanDate); err != nil {
		return ErrInvalidTask
	}
	return nil
}
