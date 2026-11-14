package domainaccount

import (
	"errors"
)

var (
	ErrDisabled = errors.New("email disabled")
	ErrLimited  = errors.New("email request limited")
	ErrToken    = errors.New("invalid verification token")
)

var (
	ErrResetToken    = errors.New("invalid password reset capability")
	ErrResetPassword = errors.New("invalid reset password")
	ErrResetEmail    = errors.New("invalid reset email")
)
