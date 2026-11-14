package domainproject

import (
	"context"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type Repository interface {
	Create(context.Context, domainaccount.UserID, *Project) (*Project, error)
	GetByID(context.Context, domainaccount.UserID, string) (*Project, error)
	List(context.Context, domainaccount.UserID) ([]Project, error)
	Update(context.Context, domainaccount.UserID, string, Change) (*Project, error)
	Delete(context.Context, domainaccount.UserID, string) error
}

type SharingRepository interface {
	Members(context.Context, domainaccount.UserID, string) ([]Member, error)
	Invite(context.Context, domainaccount.UserID, string, string, string, [32]byte) (*Invitation, error)
	Invitations(context.Context, domainaccount.UserID, string) ([]Invitation, error)
	RevokeInvitation(context.Context, domainaccount.UserID, string, string) error
	AcceptInvitation(context.Context, domainaccount.UserID, [32]byte) (string, error)
	RemoveMember(context.Context, domainaccount.UserID, string, string, bool) error
	Transfer(context.Context, domainaccount.UserID, string, string) error
}
