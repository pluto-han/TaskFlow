package domainaccount

import "errors"

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidUser  = errors.New("invalid registration data")
	ErrEmailExists  = errors.New("email already exists")
	ErrUserNotFound = errors.New("user not found")
)
