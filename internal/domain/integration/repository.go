package domainintegration

import (
	"context"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type Repository interface {
	Status(context.Context, domainaccount.UserID) (Binding, error)
	Issue(context.Context, domainaccount.UserID, []byte) (time.Time, error)
	Bind(context.Context, []byte, Target) (bool, error)
	Unbind(context.Context, domainaccount.UserID, int64) error
}
