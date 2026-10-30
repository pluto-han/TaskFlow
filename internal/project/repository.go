package project

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pluto-han/taskflow/internal/session"
)

var (
	ErrForbidden         = errors.New("project operation forbidden")
	ErrInvitationInvalid = errors.New("invalid invitation")
	ErrInvalidInvitee    = errors.New("invalid invitation recipient or owner target")
	ErrOwnerRequired     = errors.New("transfer ownership before leaving")
	ErrInvalid           = errors.New("invalid project data")
	ErrNotFound          = errors.New("project not found")
	ErrNotEmpty          = errors.New("project still contains tasks")
)

type Repository interface {
	Create(context.Context, session.UserID, *Project) (*Project, error)
	GetByID(context.Context, session.UserID, string) (*Project, error)
	List(context.Context, session.UserID) ([]Project, error)
	Update(context.Context, session.UserID, string, UpdateRequest) (*Project, error)
	Delete(context.Context, session.UserID, string) error
}

// ParseID accepts a UUID and returns its canonical text representation.
func ParseID(raw string) (string, error) {
	var value pgtype.UUID
	if err := value.Scan(raw); err != nil || !value.Valid {
		return "", ErrInvalid
	}
	return value.String(), nil
}
