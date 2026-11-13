package infraaccount

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

func restoreUser(row db.User, err error) (*domainaccount.User, error) {
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainaccount.ErrUserNotFound
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return nil, domainaccount.ErrEmailExists
		}

		return nil, fmt.Errorf("user repository: %w", err)
	}

	if !row.ID.Valid || !row.CreatedAt.Valid || row.CreatedAt.InfinityModifier != pgtype.Finite {
		return nil, errors.New("invalid user database mapping")
	}

	return domainaccount.RestoreUser(
		row.ID.String(),
		row.Email,
		row.PasswordHash,
		row.CredentialsVersion,
		row.CreatedAt.Time.UTC(),
	), nil
}
