package applicationtask

import (
	"context"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

func (s *Service) CreateToday(
	ctx context.Context,
	owner domainaccount.UserID,
	input CreateTodayInput,
) (*TaskResult, error) {
	if owner == "" {
		return nil, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := domainplanning.ParseDate(input.PlanDate); err != nil {
		return nil, domaintask.ErrInvalidTask
	}

	// One captured acceptance instant determines both plan_date and created_at.
	now := s.now()
	day := domainplanning.At(now)
	if input.PlanDate != day.PlanDate {
		return nil, domaintask.ErrPlanDayChanged
	}

	return s.create(ctx, owner, CreateInput{Title: input.Title, Description: input.Description,
		Priority: input.Priority, DueDate: input.DueDate}, &day.PlanDate, now)
}

func (s *Service) ListToday(
	ctx context.Context,
	owner domainaccount.UserID,
	options ListOptions,
) (TodayResult, error) {
	if owner == "" {
		return TodayResult{}, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return TodayResult{}, err
	}

	day := domainplanning.At(s.now())
	// The today endpoint cannot select projects or historical dates.
	if options.PlanDate != nil || options.ProjectID != nil {
		return TodayResult{}, domaintask.ErrInvalidList
	}

	var page PageResult
	var err error
	if s.todayCache != nil && todayCacheEligible(options) {
		page, day, err = s.listTodayCached(ctx, owner, day)
	} else {
		s.observeCacheRead("bypass")
		options.PlanDate = &day.PlanDate
		page, err = s.List(ctx, owner, options)
	}
	if err != nil {
		return TodayResult{}, err
	}

	return TodayResult{Items: page.Items, NextCursor: page.NextCursor, PlanDate: day.PlanDate,
		Timezone: day.Timezone, ServerTime: day.ServerTime, NextDayAt: day.NextDayAt}, nil
}
