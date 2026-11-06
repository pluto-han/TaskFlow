package recurrence

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/session"
)

type Service struct {
	schedulerEnabled bool
	repo             Repository
	now              func() time.Time
}

func NewServiceWithScheduler(repo Repository, enabled bool) *Service {
	s := NewService(repo)
	s.schedulerEnabled = enabled
	return s
}
func NewService(repo Repository) *Service { return NewServiceWithClock(repo, time.Now) }
func NewServiceWithClock(repo Repository, now func() time.Time) *Service {
	return &Service{repo: repo, now: now}
}
func check(ctx context.Context, owner session.UserID) error {
	if owner == "" {
		return session.ErrUnauthorized
	}
	return ctx.Err()
}
func validText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0) && (!required || strings.TrimSpace(s) != "")
}
func parseID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if e := id.Scan(raw); e != nil || !id.Valid {
		return id, ErrInvalid
	}
	return id, nil
}
func valid(r *Rule) bool {
	return validText(r.Title, 200, true) && validText(r.Description, 2000, false) && r.Priority >= 1 && r.Priority <= 3 && r.IntervalDays >= 1 && r.IntervalDays <= 365
}
func decorate(r *Rule, today string) *Rule {
	r.PreviewDates = []string{}
	if !r.Paused && r.DeletedAt == nil {
		from := today
		if r.NextRunDate > from {
			from = r.NextRunDate
		}
		dates, e := Dates(r.AnchorDate, r.IntervalDays, from, 5)
		if e == nil {
			r.PreviewDates = dates
		}
	}
	return r
}
func (s *Service) Preview(ctx context.Context, owner session.UserID, in PreviewRequest) (Preview, error) {
	if e := check(ctx, owner); e != nil {
		return Preview{}, e
	}
	day := planning.At(s.now())
	dates, e := Dates(in.AnchorDate, in.IntervalDays, day.PlanDate, 5)
	return Preview{PlanDate: day.PlanDate, Timezone: day.Timezone, Dates: dates}, e
}
func (s *Service) Create(ctx context.Context, owner session.UserID, in CreateRequest) (*Rule, error) {
	if e := check(ctx, owner); e != nil {
		return nil, e
	}
	today := planning.At(s.now()).PlanDate
	r := &Rule{ReminderEnabled: in.ReminderEnabled, OwnerID: owner, Title: strings.TrimSpace(in.Title), Description: in.Description, Priority: in.Priority, AnchorDate: in.AnchorDate, IntervalDays: in.IntervalDays, NextRunDate: in.AnchorDate}
	if !valid(r) || in.AnchorDate < today {
		return nil, ErrInvalid
	}
	if _, e := Dates(in.AnchorDate, in.IntervalDays, today, 5); e != nil {
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
func (s *Service) Get(ctx context.Context, owner session.UserID, id string) (*Rule, error) {
	if e := check(ctx, owner); e != nil {
		return nil, e
	}
	if _, e := parseID(id); e != nil {
		return nil, e
	}
	r, e := s.repo.Get(ctx, owner, id)
	if e != nil {
		return nil, e
	}
	return decorate(r, planning.At(s.now()).PlanDate), nil
}

type cursor struct {
	Owner session.UserID `json:"owner"`
	ID    string         `json:"id"`
}

func (s *Service) List(ctx context.Context, owner session.UserID, raw string, limit int) (Page, error) {
	if e := check(ctx, owner); e != nil {
		return Page{}, e
	}
	if limit < 1 || limit > 100 || len(raw) > 1024 {
		return Page{}, ErrInvalid
	}
	after := ""
	if raw != "" {
		b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
		var c cursor
		if e != nil || json.Unmarshal(b, &c) != nil || c.Owner != owner {
			return Page{}, ErrInvalid
		}
		if _, e = parseID(c.ID); e != nil {
			return Page{}, e
		}
		after = c.ID
	}
	rows, e := s.repo.List(ctx, owner, after, limit+1)
	if e != nil {
		return Page{}, e
	}
	day := planning.At(s.now())
	page := Page{Items: rows, PlanDate: day.PlanDate, Timezone: day.Timezone, SchedulerEnabled: s.schedulerEnabled}
	if len(rows) > limit {
		page.Items = rows[:limit]
		b, _ := json.Marshal(cursor{Owner: owner, ID: rows[limit-1].ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	if page.Items == nil {
		page.Items = []Rule{}
	}
	for i := range page.Items {
		decorate(&page.Items[i], day.PlanDate)
	}
	return page, nil
}
func (s *Service) Update(ctx context.Context, owner session.UserID, id string, in UpdateRequest) (*Rule, error) {
	if e := check(ctx, owner); e != nil {
		return nil, e
	}
	if _, e := parseID(id); e != nil {
		return nil, e
	}
	if in.Version < 1 || (in.Title == nil && in.Description == nil && in.Priority == nil && in.AnchorDate == nil && in.IntervalDays == nil && in.Paused == nil && in.ReminderEnabled == nil) {
		return nil, ErrInvalid
	}
	r, e := s.repo.Change(ctx, owner, id, in.Version, func(r *Rule, last string) error {
		today := planning.At(s.now()).PlanDate
		if in.ReminderEnabled != nil {
			r.ReminderEnabled = *in.ReminderEnabled
		}
		if in.Title != nil {
			r.Title = strings.TrimSpace(*in.Title)
		}
		if in.Description != nil {
			r.Description = *in.Description
		}
		if in.Priority != nil {
			r.Priority = *in.Priority
		}
		if in.IntervalDays != nil && *in.IntervalDays != r.IntervalDays && in.AnchorDate == nil {
			return ErrInvalid
		}
		reschedule := in.AnchorDate != nil
		if reschedule {
			if *in.AnchorDate <= today {
				return ErrInvalid
			}
			r.AnchorDate = *in.AnchorDate
		}
		if in.IntervalDays != nil {
			r.IntervalDays = *in.IntervalDays
		}
		if !valid(r) {
			return ErrInvalid
		}
		if _, e := Dates(r.AnchorDate, r.IntervalDays, today, 1); e != nil {
			return e
		}
		resume := in.Paused != nil && !*in.Paused && r.Paused
		if in.Paused != nil {
			r.Paused = *in.Paused
		}
		if reschedule || resume {
			next, e := nextAfter(today, r.AnchorDate, r.IntervalDays, last)
			if e != nil {
				return e
			}
			r.NextRunDate = next
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return decorate(r, planning.At(s.now()).PlanDate), nil
}
func (s *Service) Delete(ctx context.Context, owner session.UserID, id string, version int64) error {
	if e := check(ctx, owner); e != nil {
		return e
	}
	if _, e := parseID(id); e != nil {
		return e
	}
	if version < 1 {
		return ErrInvalid
	}
	_, e := s.repo.Change(ctx, owner, id, version, func(r *Rule, _ string) error { now := s.now(); r.DeletedAt = &now; r.Paused = true; return nil })
	return e
}
