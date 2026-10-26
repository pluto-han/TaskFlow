package user

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
)

func TestRepositoryErrorMapping(t *testing.T) {
	for _, tc := range []struct{ err, want error }{
		{pgx.ErrNoRows, ErrUserNotFound},
		{&pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}, ErrEmailExists},
	} {
		_, err := fromRow(db.User{}, tc.err)
		if !errors.Is(err, tc.want) {
			t.Fatalf("got %v want %v", err, tc.want)
		}
	}
	for _, source := range []error{
		&pgconn.PgError{Code: "23505", ConstraintName: "users_pkey"},
		&pgconn.PgError{Code: "23514", ConstraintName: "users_email_normalized"},
		errors.New("connection failed"),
	} {
		_, err := fromRow(db.User{}, source)
		if !errors.Is(err, source) || errors.Is(err, ErrEmailExists) {
			t.Fatalf("wrong mapping: %v", err)
		}
	}
	if _, err := fromRow(db.User{}, nil); err == nil {
		t.Fatal("accepted invalid UUID/time")
	}
}
