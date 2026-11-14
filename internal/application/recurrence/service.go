package applicationrecurrence

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
)

type Service struct {
	schedulerEnabled bool
	repo             domainrecurrence.Repository
	now              func() time.Time
}

func NewWithScheduler(repo domainrecurrence.Repository, enabled bool) *Service {
	s := New(repo)
	s.schedulerEnabled = enabled

	return s
}

func New(repo domainrecurrence.Repository) *Service { return NewWithClock(repo, time.Now) }

func NewWithClock(repo domainrecurrence.Repository, now func() time.Time) *Service {
	return &Service{repo: repo, now: now}
}

func check(ctx context.Context, owner domainaccount.UserID) error {
	if owner == "" {
		return domainaccount.ErrUnauthorized
	}

	return ctx.Err()
}

func decorate(rule *domainrecurrence.Rule, today string) *RuleResult {
	r := &RuleResult{
		ReminderEnabled: rule.ReminderEnabled,
		ID:              rule.ID,
		OwnerID:         rule.OwnerID,
		Title:           rule.Title,
		Description:     rule.Description,
		Priority:        rule.Priority,
		AnchorDate:      rule.AnchorDate,
		IntervalDays:    rule.IntervalDays,
		NextRunDate:     rule.NextRunDate,
		Paused:          rule.Paused,
		Version:         rule.Version,
		CreatedAt:       rule.CreatedAt,
		UpdatedAt:       rule.UpdatedAt,
		DeletedAt:       rule.DeletedAt,
	}
	r.PreviewDates = []string{}
	if !r.Paused && r.DeletedAt == nil {
		from := today
		if r.NextRunDate > from {
			from = r.NextRunDate
		}
		dates, e := domainrecurrence.Dates(r.AnchorDate, r.IntervalDays, from, 5)
		if e == nil {
			r.PreviewDates = dates
		}
	}
	return r
}

func (s *Service) Preview(
	ctx context.Context,
	owner domainaccount.UserID,
	in PreviewInput,
) (PreviewResult, error) {
	if e := check(ctx, owner); e != nil {
		return PreviewResult{}, e
	}

	day := domainplanning.At(s.now())
	dates, e := domainrecurrence.Dates(in.AnchorDate, in.IntervalDays, day.PlanDate, 5)

	return PreviewResult{PlanDate: day.PlanDate, Timezone: day.Timezone, Dates: dates}, e
}

func (s *Service) Create(
	ctx context.Context,
	owner domainaccount.UserID,
	in CreateInput,
) (*RuleResult, error) {
	if e := check(ctx, owner); e != nil {
		return nil, e
	}

	today := domainplanning.At(s.now()).PlanDate
	r, e := domainrecurrence.NewRule(owner, in.Title, in.Description, in.Priority, in.AnchorDate, in.IntervalDays, in.ReminderEnabled, today)
	if e != nil {
		return nil, e
	}

	var id [16]byte
	if _, e := rand.Read(id[:]); e != nil {
		return nil, e
	}

	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	r.ID = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	saved, e := s.repo.Create(ctx, r)
	if e != nil {
		return nil, e
	}

	return decorate(saved, today), nil
}

func (s *Service) Get(
	ctx context.Context,
	owner domainaccount.UserID,
	id string,
) (*RuleResult, error) {
	if e := check(ctx, owner); e != nil {
		return nil, e
	}
	if _, e := domainrecurrence.ParseID(id); e != nil {
		return nil, e
	}

	r, e := s.repo.Get(ctx, owner, id)
	if e != nil {
		return nil, e
	}

	return decorate(r, domainplanning.At(s.now()).PlanDate), nil
}

func (s *Service) List(
	ctx context.Context,
	owner domainaccount.UserID,
	raw string,
	limit int,
) (PageResult, error) {
	if e := check(ctx, owner); e != nil {
		return PageResult{}, e
	}
	if limit < 1 || limit > 100 || len(raw) > 1024 {
		return PageResult{}, domainrecurrence.ErrInvalid
	}

	after := ""
	if raw != "" {
		b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
		var c cursor
		if e != nil || json.Unmarshal(b, &c) != nil || c.Owner != owner {
			return PageResult{}, domainrecurrence.ErrInvalid
		}
		if _, e = domainrecurrence.ParseID(c.ID); e != nil {
			return PageResult{}, e
		}

		after = c.ID
	}
	rows, e := s.repo.List(ctx, owner, after, limit+1)
	if e != nil {
		return PageResult{}, e
	}

	day := domainplanning.At(s.now())
	items := make([]RuleResult, len(rows))
	for i := range rows {
		items[i] = *decorate(&rows[i], day.PlanDate)
	}
	page := PageResult{
		Items:            items,
		PlanDate:         day.PlanDate,
		Timezone:         day.Timezone,
		SchedulerEnabled: s.schedulerEnabled,
	}
	if len(rows) > limit {
		page.Items = items[:limit]
		b, _ := json.Marshal(cursor{Owner: owner, ID: rows[limit-1].ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	if page.Items == nil {
		page.Items = []RuleResult{}
	}
	return page, nil
}

func (s *Service) Update(
	ctx context.Context,
	owner domainaccount.UserID,
	id string,
	in UpdateInput,
) (*RuleResult, error) {
	if e := check(ctx, owner); e != nil {
		return nil, e
	}
	if _, e := domainrecurrence.ParseID(id); e != nil {
		return nil, e
	}
	if in.Version < 1 || (in.Title == nil && in.Description == nil && in.Priority == nil && in.AnchorDate == nil && in.IntervalDays == nil && in.Paused == nil && in.ReminderEnabled == nil) {
		return nil, domainrecurrence.ErrInvalid
	}

	r, e := s.repo.Change(ctx, owner, id, in.Version, func(r *domainrecurrence.Rule, last string) error {
		return r.Apply(domainrecurrence.Change(in), domainplanning.At(s.now()).PlanDate, last)
	})
	if e != nil {
		return nil, e
	}

	return decorate(r, domainplanning.At(s.now()).PlanDate), nil
}

func (s *Service) Delete(
	ctx context.Context,
	owner domainaccount.UserID,
	id string,
	version int64,
) error {
	if e := check(ctx, owner); e != nil {
		return e
	}
	if _, e := domainrecurrence.ParseID(id); e != nil {
		return e
	}
	if version < 1 {
		return domainrecurrence.ErrInvalid
	}

	_, e := s.repo.Change(ctx, owner, id, version, func(r *domainrecurrence.Rule, _ string) error {
		r.Delete(s.now())

		return nil
	})

	return e
}
