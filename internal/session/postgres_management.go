package session

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
)

type lockedCredentials struct {
	queries *db.Queries
	version int64
}

// Every session creation, revocation and password change locks user -> sessions.
// No password hashing or external call runs while this lock is held.
func (r *PostgresRepository) lockUser(ctx context.Context, id pgtype.UUID) (pgx.Tx, lockedCredentials, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, lockedCredentials{}, err
	}
	q := db.New(tx)
	version, err := q.LockUserCredentials(ctx, id)
	if err != nil {
		_ = tx.Rollback(context.Background())
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrUnauthorized
		}
		return nil, lockedCredentials{}, err
	}
	return tx, lockedCredentials{queries: q, version: version}, nil
}

func requireActive(ctx context.Context, q *db.Queries, p Principal) error {
	id, err := sessionID(p.SessionID)
	if err != nil {
		return ErrUnauthorized
	}
	uid, err := sessionID(string(p.UserID))
	if err != nil {
		return ErrUnauthorized
	}
	_, err = q.LockActiveSession(ctx, db.LockActiveSessionParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	return err
}

func (r *PostgresRepository) RevokeOwned(ctx context.Context, p Principal, target string) error {
	uid, err := sessionID(string(p.UserID))
	if err != nil {
		return ErrUnauthorized
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
	if err := requireActive(ctx, locked.queries, p); err != nil {
		return err
	}
	count, err := locked.queries.RevokeSession(ctx, db.RevokeSessionParams{ID: id, UserID: uid})
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrSessionNotFound
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) ChangePassword(ctx context.Context, p Principal, version int64, hash string) error {
	uid, err := sessionID(string(p.UserID))
	if err != nil {
		return ErrUnauthorized
	}
	tx, locked, err := r.lockUser(ctx, uid)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if locked.version != version {
		return ErrCredentialsChanged
	}
	if err := requireActive(ctx, locked.queries, p); err != nil {
		return err
	}
	count, err := locked.queries.UpdatePassword(ctx, db.UpdatePasswordParams{ID: uid, ExpectedVersion: version, PasswordHash: hash})
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrCredentialsChanged
	}
	if err := locked.queries.RevokeAllSessions(ctx, uid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) ListActive(ctx context.Context, owner UserID, query ListQuery) ([]SessionInfo, error) {
	uid, err := sessionID(string(owner))
	if err != nil {
		return nil, ErrUnauthorized
	}
	params := db.ListActiveSessionsParams{UserID: uid, Limit: int32(query.Limit + 1),
		AfterCreatedAt: pgtype.Timestamptz{Time: time.Time{}, Valid: true}, AfterID: pgtype.UUID{Valid: true}}
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
	items := make([]SessionInfo, 0, len(rows))
	for _, row := range rows {
		if !row.ID.Valid || !row.CreatedAt.Valid || !row.ExpiresAt.Valid || row.CreatedAt.InfinityModifier != pgtype.Finite || row.ExpiresAt.InfinityModifier != pgtype.Finite {
			return nil, errors.New("invalid session listing mapping")
		}
		items = append(items, SessionInfo{ID: row.ID.String(), CreatedAt: row.CreatedAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(), CredentialKind: row.CredentialKind})
	}
	return items, nil
}
