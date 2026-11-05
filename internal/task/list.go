package task

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/session"
)

var ErrInvalidList = errors.New("invalid task list parameters")

const DefaultLimit = 20
const MaxLimit = 100

type ListFilter struct {
	PlanDate   *string    `json:"plan_date,omitempty"`
	ProjectID  *string    `json:"project_id,omitempty"`
	Completed  *bool      `json:"completed,omitempty"`
	Priority   *int       `json:"priority,omitempty"`
	HasDueDate *bool      `json:"has_due_date,omitempty"`
	DueFrom    *time.Time `json:"due_from,omitempty"`
	DueBefore  *time.Time `json:"due_before,omitempty"`
}

type ListOptions struct {
	ListFilter
	Limit  int
	Cursor string
}

type CursorPosition struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

// PageQuery is the bounded repository contract. Limit includes the lookahead row.
type PageQuery struct {
	ListFilter
	Limit int
	After *CursorPosition
}

type TaskPage struct {
	Items      []Task
	NextCursor string
}

type taskCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"scope"`
	CursorPosition
}

func normalizeFilter(f ListFilter) (ListFilter, error) {
	if f.PlanDate != nil {
		if _, err := planning.ParseDate(*f.PlanDate); err != nil {
			return f, ErrInvalidList
		}
	}
	if f.ProjectID != nil && *f.ProjectID != "none" {
		id, err := project.ParseID(*f.ProjectID)
		if err != nil {
			return f, ErrInvalidList
		}
		f.ProjectID = &id
	}

	if f.Priority != nil && (*f.Priority < 1 || *f.Priority > 3) {
		return f, ErrInvalidList
	}
	if f.DueFrom != nil {
		value := f.DueFrom.UTC().Truncate(time.Microsecond)
		f.DueFrom = &value
	}
	if f.DueBefore != nil {
		value := f.DueBefore.UTC().Truncate(time.Microsecond)
		f.DueBefore = &value
	}
	if f.DueFrom != nil && f.DueBefore != nil && !f.DueFrom.Before(*f.DueBefore) {
		return f, ErrInvalidList
	}
	if f.HasDueDate != nil && !*f.HasDueDate && (f.DueFrom != nil || f.DueBefore != nil) {
		return f, ErrInvalidList
	}
	for _, date := range []*time.Time{f.DueFrom, f.DueBefore} {
		if !validDate(date) {
			return f, ErrInvalidList
		}
	}
	return f, nil
}

func validDate(t *time.Time) bool { return t == nil || (t.UTC().Year() >= 1 && t.UTC().Year() <= 9999) }

func validatePageQuery(q PageQuery) error {
	if q.Limit < 1 || q.Limit > MaxLimit+1 {
		return ErrInvalidList
	}
	if _, err := normalizeFilter(q.ListFilter); err != nil {
		return err
	}
	if q.After != nil && (q.After.CreatedAt.IsZero() || !validDate(&q.After.CreatedAt) || len(q.After.ID) == 0 || len(q.After.ID) > 128 || strings.ContainsRune(q.After.ID, 0)) {
		return ErrInvalidList
	}
	return nil
}

func cursorScope(owner session.UserID, f ListFilter) (string, error) {
	raw, err := json.Marshal(struct {
		Owner  session.UserID
		Filter ListFilter
	}{owner, f})
	if err != nil {
		return "", ErrInvalidList
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func decodeCursor(encoded, scope string) (*CursorPosition, error) {
	if len(encoded) > 2048 {
		return nil, ErrInvalidList
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidList
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value taskCursor
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidList
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, ErrInvalidList
	}
	if value.Version != 1 || value.Scope != scope {
		return nil, ErrInvalidList
	}
	if err := validatePageQuery(PageQuery{Limit: 1, After: &value.CursorPosition}); err != nil {
		return nil, err
	}
	return &value.CursorPosition, nil
}

func encodeCursor(position CursorPosition, scope string) (string, error) {
	raw, err := json.Marshal(taskCursor{Version: 1, Scope: scope, CursorPosition: position})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ParseListOptions rejects empty/duplicate known parameters. Unknown parameters
// cannot select a user; identity is always taken from authenticated context.
func ParseListOptions(rawQuery string) (ListOptions, error) {
	values, err := url.ParseQuery(rawQuery)
	options := ListOptions{Limit: DefaultLimit}
	if err != nil {
		return options, ErrInvalidList
	}
	for _, key := range []string{"limit", "cursor", "completed", "priority", "has_due_date", "due_from", "due_before", "project_id", "plan_date"} {
		v, exists := values[key]
		if !exists {
			continue
		}
		if len(v) != 1 || v[0] == "" {
			return options, ErrInvalidList
		}
		switch key {
		case "plan_date":
			value := v[0]
			options.PlanDate = &value
		case "project_id":
			value := v[0]
			options.ProjectID = &value
		case "limit":
			options.Limit, err = strconv.Atoi(v[0])
			if err != nil || options.Limit < 1 || options.Limit > MaxLimit {
				return options, ErrInvalidList
			}
		case "cursor":
			if len(v[0]) > 2048 {
				return options, ErrInvalidList
			}
			options.Cursor = v[0]
		case "priority":
			number, e := strconv.Atoi(v[0])
			if e != nil {
				return options, ErrInvalidList
			}
			options.Priority = &number
		case "completed", "has_due_date":
			if v[0] != "true" && v[0] != "false" {
				return options, ErrInvalidList
			}
			value := v[0] == "true"
			if key == "completed" {
				options.Completed = &value
			} else {
				options.HasDueDate = &value
			}
		case "due_from", "due_before":
			value, e := time.Parse(time.RFC3339Nano, v[0])
			if e != nil {
				return options, ErrInvalidList
			}
			if key == "due_from" {
				options.DueFrom = &value
			} else {
				options.DueBefore = &value
			}
		}
	}
	options.ListFilter, err = normalizeFilter(options.ListFilter)
	return options, err
}
