package infrasession

import (
	"context"
	"errors"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

type LockedCredentials struct {
	queries *db.Queries
	version int64
}

// Every session creation, revocation and password change locks user -> sessions.
// No password hashing or external call runs while this lock is held.
func (r *Repository) lockUser(
	ctx context.Context,
	id pgtype.UUID,
) (pgx.Tx, LockedCredentials, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, LockedCredentials{}, err
	}

	q := db.New(tx)
	version, err := q.LockUserCredentials(ctx, id)
	if err != nil {
		_ = tx.Rollback(context.Background())
		if errors.Is(err, pgx.ErrNoRows) {
			err = domainaccount.ErrUnauthorized
		}
		return nil, LockedCredentials{}, err
	}

	return tx, LockedCredentials{queries: q, version: version}, nil
}

func RequireActive(ctx context.Context, q *db.Queries, p domainsession.Principal) error {
	id, err := sessionID(p.SessionID)
	if err != nil {
		return domainaccount.ErrUnauthorized
	}

	uid, err := sessionID(string(p.UserID))
	if err != nil {
		return domainaccount.ErrUnauthorized
	}

	_, err = q.LockActiveSession(ctx, db.LockActiveSessionParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domainaccount.ErrUnauthorized
	}

	return err
}

func (r *Repository) RevokeOwned(
	ctx context.Context,
	p domainsession.Principal,
	target string,
) error {
	uid, err := sessionID(string(p.UserID))
	if err != nil {
		return domainaccount.ErrUnauthorized
	}

	id, err := sessionID(target)
	if err != nil {
		return err
	}

	tx, locked, err := r.lockUser(ctx, uid)
	if err != nil {
		return err
	}

	defer tx.Rollback(context.Background())
	if err := RequireActive(ctx, locked.queries, p); err != nil {
		return err
	}

	count, err := locked.queries.RevokeSession(ctx, db.RevokeSessionParams{ID: id, UserID: uid})
	if err != nil {
		return err
	}
	if count == 0 {
		return domainsession.ErrSessionNotFound
	}

	return tx.Commit(ctx)
}

func (r *Repository) ChangePassword(
	ctx context.Context,
	p domainsession.Principal,
	version int64,
	hash string,
) error {
	uid, err := sessionID(string(p.UserID))
	if err != nil {
		return domainaccount.ErrUnauthorized
	}

	tx, locked, err := r.lockUser(ctx, uid)
	if err != nil {
		return err
	}

	defer tx.Rollback(context.Background())
	if locked.version != version {
		return domainsession.ErrCredentialsChanged
	}
	if err := RequireActive(ctx, locked.queries, p); err != nil {
		return err
	}

	count, err := locked.queries.UpdatePassword(ctx, db.UpdatePasswordParams{
		ID:              uid,
		ExpectedVersion: version,
		PasswordHash:    hash,
	})
	if err != nil {
		return err
	}
	if count != 1 {
		return domainsession.ErrCredentialsChanged
	}
	if err := locked.queries.RevokeAllSessions(ctx, uid); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) ListActive(
	ctx context.Context,
	owner domainaccount.UserID,
	query domainsession.ListQuery,
) ([]domainsession.SessionInfo, error) {
	uid, err := sessionID(string(owner))
	if err != nil {
		return nil, domainaccount.ErrUnauthorized
	}

	params := db.ListActiveSessionsParams{UserID: uid, Limit: int32(query.Limit + 1),
		AfterCreatedAt: pgtype.Timestamptz{
			Time:  time.Time{},
			Valid: true,
		}, AfterID: pgtype.UUID{Valid: true}}
	if query.After != nil {
		params.HasCursor = true
		params.AfterCreatedAt = pgtype.Timestamptz{Time: query.After.CreatedAt, Valid: true}
		params.AfterID, err = sessionID(query.After.ID)
		if err != nil {
			return nil, err
		}
	}
	rows, err := r.queries.ListActiveSessions(ctx, params)
	if err != nil {
		return nil, err
	}

	items := make([]domainsession.SessionInfo, 0, len(rows))
	for _, row := range rows {
		if !row.ID.Valid || !row.CreatedAt.Valid || !row.ExpiresAt.Valid || row.CreatedAt.InfinityModifier != pgtype.Finite || row.ExpiresAt.InfinityModifier != pgtype.Finite {
			return nil, errors.New("invalid session listing mapping")
		}

		items = append(items, domainsession.SessionInfo{
			ID:             row.ID.String(),
			CreatedAt:      row.CreatedAt.Time.UTC(),
			ExpiresAt:      row.ExpiresAt.Time.UTC(),
			CredentialKind: row.CredentialKind,
		})
	}

	return items, nil
}
