package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type fakeRepository struct {
	domainaccount.Repository

	saved *domainaccount.User
	err   error
	calls int
}

type fakeHasher struct {
	calls    int
	password string
	err      error
}

func TestRegisterNormalizesEmailAndPreservesPassword(t *testing.T) {
	r, h := &fakeRepository{}, &fakeHasher{}
	input := applicationaccount.CreateInput{
		Email:    "  Alice@Example.COM  ",
		Password: "  a long passphrase  ",
	}
	u, err := applicationaccount.New(r, h).Register(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	var uuid pgtype.UUID
	if err := uuid.Scan(u.ID); err != nil ||
		!uuid.Valid ||
		uuid.Bytes[6]>>4 != 4 ||
		uuid.Bytes[8]>>6 != 2 {
		t.Fatalf("invalid v4 UUID: %s", u.ID)
	}

	if u.Email != "alice@example.com" ||
		h.password != input.Password ||
		r.saved.PasswordHash != "test-only-hash" ||
		u.CreatedAt.IsZero() {
		t.Fatalf("unexpected registration result")
	}
	raw, _ := json.Marshal(u)
	if strings.Contains(string(raw), r.saved.PasswordHash) {
		t.Fatal("profile leaks hash")
	}
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name, email, password string
		valid                 bool
	}{
		{
			name:     "minimum",
			email:    "a@example.com",
			password: strings.Repeat("a", 8),
			valid:    true,
		},
		{
			name:     "unicode minimum",
			email:    "a@example.com",
			password: strings.Repeat("🔑", 8),
			valid:    true,
		},
		{
			name:     "unicode short",
			email:    "a@example.com",
			password: strings.Repeat("🔑", 7),
			valid:    false,
		},
		{
			name:     "maximum",
			email:    "a@example.com",
			password: strings.Repeat("a", 128),
			valid:    true,
		},
		{
			name:     "unicode max bytes",
			email:    "a@example.com",
			password: strings.Repeat("🔑", 128),
			valid:    true,
		},
		{
			name:     "short",
			email:    "a@example.com",
			password: strings.Repeat("a", 7),
			valid:    false,
		},
		{
			name:     "long",
			email:    "a@example.com",
			password: strings.Repeat("a", 129),
			valid:    false,
		},
		{
			name:     "too many bytes",
			email:    "a@example.com",
			password: strings.Repeat("🔑", 129),
			valid:    false,
		},
		{
			name:     "blank",
			email:    "a@example.com",
			password: strings.Repeat(" ", 8),
			valid:    false,
		},
		{
			name:     "invalid utf8",
			email:    "a@example.com",
			password: strings.Repeat("a", 15) + "\xff",
			valid:    false,
		},
		{
			name:     "empty email",
			email:    "",
			password: "a long passphrase",
			valid:    false,
		},
		{
			name:     "display name",
			email:    "Alice <a@example.com>",
			password: "a long passphrase",
			valid:    false,
		},
		{
			name:     "unicode email",
			email:    "用户@example.com",
			password: "a long passphrase",
			valid:    false,
		},
		{
			name:     "invalid domain",
			email:    "a@-example.com",
			password: "a long passphrase",
			valid:    false,
		},
		{
			name:     "missing domain",
			email:    "a@",
			password: "a long passphrase",
			valid:    false,
		},
		{
			name:     "long local",
			email:    strings.Repeat("a", 65) + "@example.com",
			password: "a long passphrase",
			valid:    false,
		},
		{
			name:     "long email",
			email:    "a@" + strings.Repeat("b", 250) + ".com",
			password: "a long passphrase",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, h := &fakeRepository{}, &fakeHasher{}
			_, err := applicationaccount.New(r, h).Register(context.Background(), applicationaccount.CreateInput{
				Email:    tt.email,
				Password: tt.password,
			})
			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, domainaccount.ErrInvalidUser) || h.calls != 0 || r.calls != 0 {
				t.Fatalf("validation = %v, hash calls %d, db calls %d", err, h.calls, r.calls)
			}
		})
	}
}

func TestRegisterFailures(t *testing.T) {
	input := applicationaccount.CreateInput{
		Email:    "a@example.com",
		Password: "a long passphrase",
	}

	tests := []error{
		domainaccount.ErrEmailExists,
		errors.New("database failure"),
	}

	for _, want := range tests {
		_, err := applicationaccount.New(&fakeRepository{err: want}, &fakeHasher{}).Register(context.Background(), input)
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
	}

	want := errors.New("random source failure")
	r := &fakeRepository{}
	if _, err := applicationaccount.New(r, &fakeHasher{err: want}).Register(context.Background(), input); !errors.Is(err, want) ||
		r.calls != 0 {
		t.Fatalf("hash failure: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h := &fakeHasher{}
	if _, err := applicationaccount.New(r, h).Register(ctx, input); !errors.Is(err, context.Canceled) ||
		h.calls != 0 ||
		r.calls != 0 {
		t.Fatalf("canceled: %v", err)
	}
}

func (r *fakeRepository) Create(
	ctx context.Context,
	u *domainaccount.User,
) (*domainaccount.User, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}

	saved := *u
	saved.CreatedAt = time.Now().UTC()
	r.saved = &saved

	return &saved, nil
}

func (h *fakeHasher) HashPassword(ctx context.Context, p string) (string, error) {
	h.calls++
	h.password = p
	return "test-only-hash", h.err
}
