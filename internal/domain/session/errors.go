package domainsession

import (
	"errors"
)

var (
	ErrSessionNotFound       = errors.New("session not found")
	ErrInvalidSessionQuery   = errors.New("invalid session query")
	ErrInvalidPasswordChange = errors.New("invalid password change")
	ErrCurrentPassword       = errors.New("incorrect current password")
	ErrCredentialsChanged    = errors.New("credentials changed")
)
