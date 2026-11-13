package infraaccount

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type Repository struct {
	queries *db.Queries
}

var _ domainaccount.Repository = (*Repository)(nil)

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: db.New(pool)}
}

func (r *Repository) Create(
	ctx context.Context,
	u *domainaccount.User,
) (*domainaccount.User, error) {
	var id pgtype.UUID
	if err := id.Scan(u.ID); err != nil {
		return nil, fmt.Errorf("user UUID: %w", err)
	}

	return restoreUser(r.queries.CreateUser(ctx, db.CreateUserParams{
		ID:           id,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
	}))
}

func (r *Repository) GetByEmail(ctx context.Context, email string) (*domainaccount.User, error) {
	return restoreUser(r.queries.GetUserByEmail(ctx, email))
}

func (r *Repository) GetByID(ctx context.Context, rawID string) (*domainaccount.User, error) {
	var id pgtype.UUID
	if err := id.Scan(rawID); err != nil {
		return nil, domainaccount.ErrUserNotFound
	}

	return restoreUser(r.queries.GetUserByID(ctx, id))
}
