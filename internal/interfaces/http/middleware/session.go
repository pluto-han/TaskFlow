package interfaceshttpmiddleware

import (
	"context"

	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

type principalKey struct{}

// CurrentPrincipal reads the authenticated identity from a typed context key.
func CurrentPrincipal(ctx context.Context) (domainsession.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(domainsession.Principal)

	return p, ok
}

// WithPrincipal attaches the identity established by a session authenticator.
func WithPrincipal(ctx context.Context, p domainsession.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
