package domainsession

import (
	"context"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type Repository interface {
	Create(context.Context, string, domainaccount.UserID, [32]byte, int64) (Principal, error)
	FindActive(context.Context, [32]byte) (Principal, error)
	CreateBrowser(context.Context, string, domainaccount.UserID, [32]byte, [32]byte, int64) (Principal, error)
	FindBrowser(context.Context, [32]byte) (BrowserPrincipal, error)
	Revoke(context.Context, Principal) error
	ListActive(context.Context, domainaccount.UserID, ListQuery) ([]SessionInfo, error)
	RevokeOwned(context.Context, Principal, string) error
	ChangePassword(context.Context, Principal, int64, string) error
}
