package user

import "errors"

var (
	ErrInvalidUser  = errors.New("invalid registration data")
	ErrEmailExists  = errors.New("email already exists")
	ErrUserNotFound = errors.New("user not found")
)
