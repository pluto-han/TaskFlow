package domainrecurrence

import (
	"context"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

// Change executes under the same row lock future schedulers must acquire.
// lastDate is the last receipt, so resuming never recreates an already-run day.
type Repository interface {
	Create(context.Context, *Rule) (*Rule, error)
	Get(context.Context, domainaccount.UserID, string) (*Rule, error)
	List(context.Context, domainaccount.UserID, string, int) ([]Rule, error)
	Change(context.Context, domainaccount.UserID, string, int64, func(*Rule, string) error) (*Rule, error)
}
