package domainproject

import (
	"errors"
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
