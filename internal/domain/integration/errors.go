package domainintegration

import (
	"errors"
)

var (
	ErrInvalid  = errors.New("invalid binding data")
	ErrCode     = errors.New("invalid or expired binding code")
	ErrLimited  = errors.New("binding code issuance limited")
	ErrConflict = errors.New("binding version changed")
)
