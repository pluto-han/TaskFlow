package infrasession

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

type Repository struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: db.New(pool), pool: pool}
}

func (r *Repository) Create(
	ctx context.Context,
	id string,
	uid domainaccount.UserID,
	hash [32]byte,
	version int64,
) (domainsession.Principal, error) {
	var sid, userID pgtype.UUID
	if err := sid.Scan(id); err != nil {
		return domainsession.Principal{}, err
	}
	if err := userID.Scan(string(uid)); err != nil {
		return domainsession.Principal{}, err
	}

	tx, q, err := r.lockUser(ctx, userID)
	if err != nil {
		return domainsession.Principal{}, err
	}

	defer tx.Rollback(context.Background())
	if q.version != version {
		return domainsession.Principal{}, domainaccount.ErrUnauthorized
	}

	row, err := q.queries.CreateSession(ctx, db.CreateSessionParams{
		ID:        sid,
		UserID:    userID,
		TokenHash: hash[:],
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return principalFromRow(row.ID, row.UserID, row.ExpiresAt, err)
}

func (r *Repository) FindActive(
	ctx context.Context,
	hash [32]byte,
) (domainsession.Principal, error) {
	row, err := r.queries.GetActiveSession(ctx, hash[:])

	return principalFromRow(row.ID, row.UserID, row.ExpiresAt, err)
}

func (r *Repository) CreateBrowser(
	ctx context.Context,
	id string,
	uid domainaccount.UserID,
	hash, csrf [32]byte,
	version int64,
) (domainsession.Principal, error) {
	var sid, userID pgtype.UUID
	if err := sid.Scan(id); err != nil {
		return domainsession.Principal{}, err
	}
	if err := userID.Scan(string(uid)); err != nil {
		return domainsession.Principal{}, err
	}

	tx, q, err := r.lockUser(ctx, userID)
	if err != nil {
		return domainsession.Principal{}, err
	}

	defer tx.Rollback(context.Background())
	if q.version != version {
		return domainsession.Principal{}, domainaccount.ErrUnauthorized
	}

	row, err := q.queries.CreateBrowserSession(ctx, db.CreateBrowserSessionParams{
		ID: sid, UserID: userID, TokenHash: hash[:], CsrfSecret: csrf[:],
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return principalFromRow(row.ID, row.UserID, row.ExpiresAt, err)
}

func (r *Repository) FindBrowser(
	ctx context.Context,
	hash [32]byte,
) (domainsession.BrowserPrincipal, error) {
	row, err := r.queries.GetActiveBrowserSession(ctx, hash[:])
	p, err := principalFromRow(row.ID, row.UserID, row.ExpiresAt, err)
	if err != nil {
		return domainsession.BrowserPrincipal{}, err
	}
	if len(row.CsrfSecret) != 32 {
		return domainsession.BrowserPrincipal{}, errors.New("invalid browser session mapping")
	}

	result := domainsession.BrowserPrincipal{Principal: p}
	copy(result.CSRFSecret[:], row.CsrfSecret)

	return result, nil
}

func (r *Repository) Revoke(ctx context.Context, p domainsession.Principal) error {
	return r.RevokeOwned(ctx, p, p.SessionID)
}

var _ domainsession.Repository = (*Repository)(nil)
