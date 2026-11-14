package applicationtask

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

const DefaultLimit = 20

const MaxLimit = domaintask.MaxLimit

type ListOptions struct {
	domaintask.ListFilter
	Limit  int
	Cursor string
}

type taskCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"scope"`
	cursorPosition
}

func cursorScope(owner domainaccount.UserID, f domaintask.ListFilter) (string, error) {
	raw, err := json.Marshal(struct {
		Owner  domainaccount.UserID
		Filter cursorFilter
	}{owner, cursorFilter(f)})
	if err != nil {
		return "", domaintask.ErrInvalidList
	}

	hash := sha256.Sum256(raw)

	return hex.EncodeToString(hash[:]), nil
}

func decodeCursor(encoded, scope string) (*domaintask.CursorPosition, error) {
	if len(encoded) > 2048 {
		return nil, domaintask.ErrInvalidList
	}

	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, domaintask.ErrInvalidList
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value taskCursor
	if err := decoder.Decode(&value); err != nil {
		return nil, domaintask.ErrInvalidList
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, domaintask.ErrInvalidList
	}
	if value.Version != 1 || value.Scope != scope {
		return nil, domaintask.ErrInvalidList
	}

	position := domaintask.CursorPosition(value.cursorPosition)
	if err := domaintask.ValidatePageQuery(domaintask.PageQuery{
		Limit: 1,
		After: &position,
	}); err != nil {
		return nil, err
	}

	return &position, nil
}

func encodeCursor(position domaintask.CursorPosition, scope string) (string, error) {
	raw, err := json.Marshal(taskCursor{
		Version:        1,
		Scope:          scope,
		cursorPosition: cursorPosition(position),
	})
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type cursorFilter struct {
	PlanDate   *string    `json:"plan_date,omitempty"`
	ProjectID  *string    `json:"project_id,omitempty"`
	Completed  *bool      `json:"completed,omitempty"`
	Priority   *int       `json:"priority,omitempty"`
	HasDueDate *bool      `json:"has_due_date,omitempty"`
	DueFrom    *time.Time `json:"due_from,omitempty"`
	DueBefore  *time.Time `json:"due_before,omitempty"`
}

type cursorPosition struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}
