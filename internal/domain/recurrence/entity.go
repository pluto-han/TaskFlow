package domainrecurrence

import (
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type Rule struct {
	ReminderEnabled bool
	ID              string
	OwnerID         domainaccount.UserID
	Title           string
	Description     string
	Priority        int
	AnchorDate      string
	IntervalDays    int
	NextRunDate     string
	Paused          bool
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

func validText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0) && (!required || strings.TrimSpace(s) != "")
}

func Valid(r *Rule) bool {
	return validText(r.Title, 200, true) && validText(r.Description, 2000, false) && r.Priority >= 1 && r.Priority <= 3 && r.IntervalDays >= 1 && r.IntervalDays <= 365
}

func ParseID(raw string) (string, error) {

	compact := raw
	if len(raw) == 36 {
		compact = raw[:8] + raw[9:13] + raw[14:18] + raw[19:23] + raw[24:]
	} else if len(raw) != 32 {
		return "", ErrInvalid
	}
	if _, err := hex.DecodeString(compact); err != nil {
		return "", ErrInvalid
	}

	compact = strings.ToLower(compact)

	return compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:], nil

}

type Change struct {
	ReminderEnabled *bool
	Version         int64
	Title           *string
	Description     *string
	Priority        *int
	AnchorDate      *string
	IntervalDays    *int
	Paused          *bool
}

func (r *Rule) Apply(in Change, today, last string) error {
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
	if !Valid(r) {
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
		next, e := NextAfter(today, r.AnchorDate, r.IntervalDays, last)
		if e != nil {
			return e
		}

		r.NextRunDate = next
	}
	return nil
}

func (r *Rule) Delete(now time.Time) { r.DeletedAt = &now; r.Paused = true }

func NewRule(
	owner domainaccount.UserID,
	title, description string,
	priority int,
	anchor string,
	interval int,
	reminder bool,
	today string,
) (*Rule, error) {
	r := &Rule{
		ReminderEnabled: reminder,
		OwnerID:         owner,
		Title:           strings.TrimSpace(title),
		Description:     description,
		Priority:        priority,
		AnchorDate:      anchor,
		IntervalDays:    interval,
		NextRunDate:     anchor,
	}
	if !Valid(r) || anchor < today {
		return nil, ErrInvalid
	}
	if _, e := Dates(anchor, interval, today, 5); e != nil {
		return nil, e
	}

	return r, nil
}
