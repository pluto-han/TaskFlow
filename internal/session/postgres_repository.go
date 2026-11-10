package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
)

type PostgresRepository struct{ queries *db.Queries }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{queries: db.New(pool)}
}

func mapPrincipal(id, uid pgtype.UUID, expiry pgtype.Timestamptz, err error) (Principal, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("session repository: %w", err)
	}
	if !id.Valid || !uid.Valid || !expiry.Valid || expiry.InfinityModifier != pgtype.Finite {
		return Principal{}, errors.New("invalid session mapping")
	}
	return Principal{SessionID: id.String(), UserID: UserID(uid.String()), ExpiresAt: expiry.Time.UTC()}, nil
}

func (r *PostgresRepository) Create(ctx context.Context, id string, uid UserID, hash [32]byte) (Principal, error) {
	var sid, userID pgtype.UUID
	if err := sid.Scan(id); err != nil {
		return Principal{}, err
	}
	if err := userID.Scan(string(uid)); err != nil {
		return Principal{}, err
	}
	row, err := r.queries.CreateSession(ctx, db.CreateSessionParams{ID: sid, UserID: userID, TokenHash: hash[:]})
	return mapPrincipal(row.ID, row.UserID, row.ExpiresAt, err)
}
func (r *PostgresRepository) FindActive(ctx context.Context, hash [32]byte) (Principal, error) {
	row, err := r.queries.GetActiveSession(ctx, hash[:])
	return mapPrincipal(row.ID, row.UserID, row.ExpiresAt, err)
}

func (r *PostgresRepository) CreateBrowser(ctx context.Context, id string, uid UserID, hash, csrf [32]byte) (Principal, error) {
	var sid, userID pgtype.UUID
	if err := sid.Scan(id); err != nil {
		return Principal{}, err
	}
	if err := userID.Scan(string(uid)); err != nil {
		return Principal{}, err
	}
	row, err := r.queries.CreateBrowserSession(ctx, db.CreateBrowserSessionParams{
		ID: sid, UserID: userID, TokenHash: hash[:], CsrfSecret: csrf[:],
	})
	return mapPrincipal(row.ID, row.UserID, row.ExpiresAt, err)
}

func (r *PostgresRepository) FindBrowser(ctx context.Context, hash [32]byte) (BrowserPrincipal, error) {
	row, err := r.queries.GetActiveBrowserSession(ctx, hash[:])
	p, err := mapPrincipal(row.ID, row.UserID, row.ExpiresAt, err)
	if err != nil {
		return BrowserPrincipal{}, err
	}
	if len(row.CsrfSecret) != 32 {
		return BrowserPrincipal{}, errors.New("invalid browser session mapping")
	}
	result := BrowserPrincipal{Principal: p}
	copy(result.CSRFSecret[:], row.CsrfSecret)
	return result, nil
}
func (r *PostgresRepository) Revoke(ctx context.Context, p Principal) error {
	var id, uid pgtype.UUID
	if err := id.Scan(p.SessionID); err != nil {
		return err
	}
	if err := uid.Scan(string(p.UserID)); err != nil {
		return err
	}
	count, err := r.queries.RevokeSession(ctx, db.RevokeSessionParams{ID: id, UserID: uid})
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if count == 0 {
		return ErrUnauthorized
	}
	return nil
}

var _ Repository = (*PostgresRepository)(nil)
