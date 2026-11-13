package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	infraaccount "github.com/pluto-han/taskflow/internal/infra/persistence/account"
)

// Exercise error mapping through the public repository against real constraints.
func TestRepositoryErrorMapping(t *testing.T) {
	ctx, pool := accountDatabase(t)
	repo := infraaccount.New(pool)
	const (
		id      = "00000000-0000-4000-8000-000000000902"
		otherID = "00000000-0000-4000-8000-000000000903"
	)
	seed := domainaccount.User{
		ID:           id,
		Email:        "repository-mapping@example.com",
		PasswordHash: "fixture-only-hash",
	}
	if _, err := repo.Create(ctx, &seed); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.GetByID(ctx, otherID); !errors.Is(err, domainaccount.ErrUserNotFound) {
		t.Fatalf("missing row: %v", err)
	}
	if _, err := repo.GetByID(ctx, "invalid"); !errors.Is(err, domainaccount.ErrUserNotFound) {
		t.Fatalf("invalid UUID: %v", err)
	}

	tests := []struct {
		name, id, email, code, constraint string
		duplicateEmail                    bool
	}{
		{
			name:           "email conflict",
			id:             otherID,
			email:          seed.Email,
			code:           "23505",
			constraint:     "users_email_key",
			duplicateEmail: true,
		},
		{
			name:           "primary key conflict",
			id:             id,
			email:          "different@example.com",
			code:           "23505",
			constraint:     "users_pkey",
			duplicateEmail: false,
		},
		{
			name:           "email constraint",
			id:             otherID,
			email:          " Upper@Example.com ",
			code:           "23514",
			constraint:     "users_email_normalized",
			duplicateEmail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.Create(ctx, &domainaccount.User{
				ID:           tt.id,
				Email:        tt.email,
				PasswordHash: seed.PasswordHash,
			})
			if tt.duplicateEmail {
				if !errors.Is(err, domainaccount.ErrEmailExists) {
					t.Fatalf("email conflict: %v", err)
				}
				return
			}

			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) ||
				pgErr.Code != tt.code ||
				pgErr.ConstraintName != tt.constraint ||
				errors.Is(err, domainaccount.ErrEmailExists) {
				t.Fatalf("wrong constraint mapping: %v", err)
			}
		})
	}

	if _, err := pool.Exec(ctx, "UPDATE users SET created_at='infinity' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}

	if user, err := repo.GetByID(ctx, id); err == nil || user != nil {
		t.Fatalf("accepted invalid stored timestamp: %+v, %v", user, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.GetByEmail(canceled, seed.Email); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query: %v", err)
	}

	pool.Close()
	if user, err := repo.GetByEmail(ctx, seed.Email); err == nil ||
		user != nil ||
		errors.Is(err, domainaccount.ErrEmailExists) ||
		errors.Is(err, domainaccount.ErrUserNotFound) {
		t.Fatalf("connection failure misclassified: %+v, %v", user, err)
	}
}
