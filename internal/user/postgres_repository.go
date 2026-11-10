package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
)

type PostgresRepository struct{ queries *db.Queries }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{queries: db.New(pool)}
}

func fromRow(row db.User, err error) (*User, error) {
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return nil, ErrEmailExists
		}
		return nil, fmt.Errorf("user repository: %w", err)
	}
	if !row.ID.Valid || !row.CreatedAt.Valid || row.CreatedAt.InfinityModifier != pgtype.Finite {
		return nil, errors.New("invalid user database mapping")
	}
	return &User{ID: row.ID.String(), Email: row.Email, PasswordHash: row.PasswordHash, CredentialsVersion: row.CredentialsVersion, CreatedAt: row.CreatedAt.Time.UTC()}, nil
}

func (r *PostgresRepository) Create(ctx context.Context, u *User) (*User, error) {
	var id pgtype.UUID
	if err := id.Scan(u.ID); err != nil {
		return nil, fmt.Errorf("user UUID: %w", err)
	}
	return fromRow(r.queries.CreateUser(ctx, db.CreateUserParams{ID: id, Email: u.Email, PasswordHash: u.PasswordHash}))
}

func (r *PostgresRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	return fromRow(r.queries.GetUserByEmail(ctx, email))
}

func (r *PostgresRepository) GetByID(ctx context.Context, rawID string) (*User, error) {
	var id pgtype.UUID
	if err := id.Scan(rawID); err != nil {
		return nil, ErrUserNotFound
	}
	return fromRow(r.queries.GetUserByID(ctx, id))
}

var _ Repository = (*PostgresRepository)(nil)
