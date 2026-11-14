package domainrecurrence

import (
	"errors"
)

var (
	ErrInvalid  = errors.New("invalid recurrence")
	ErrNotFound = errors.New("recurrence not found")
	ErrConflict = errors.New("recurrence version conflict")
)
