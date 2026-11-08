package user

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type fakeRepository struct {
	Repository
	saved *User
	err   error
	calls int
}

func (r *fakeRepository) Create(ctx context.Context, u *User) (*User, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	copy := *u
	copy.CreatedAt = time.Now().UTC()
	r.saved = &copy
	return &copy, nil
}

type fakeHasher struct {
	calls    int
	password string
	err      error
}

func (h *fakeHasher) HashPassword(ctx context.Context, p string) (string, error) {
	h.calls++
	h.password = p
	return "test-only-hash", h.err
}

func TestRegisterNormalizesEmailAndPreservesPassword(t *testing.T) {
	r, h := &fakeRepository{}, &fakeHasher{}
	input := RegisterRequest{Email: "  Alice@Example.COM  ", Password: "  a long passphrase  "}
	u, err := NewService(r, h).Register(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var uuid pgtype.UUID
	if err := uuid.Scan(u.ID); err != nil || !uuid.Valid || uuid.Bytes[6]>>4 != 4 || uuid.Bytes[8]>>6 != 2 {
		t.Fatalf("invalid v4 UUID: %s", u.ID)
	}
	if u.Email != "alice@example.com" || h.password != input.Password || u.PasswordHash != "test-only-hash" || u.CreatedAt.IsZero() {
		t.Fatalf("unexpected registration result")
	}
	raw, _ := json.Marshal(u)
	if strings.Contains(string(raw), u.PasswordHash) {
		t.Fatal("domain leaks hash")
	}
}

func TestRegisterValidation(t *testing.T) {
	for _, tc := range []struct {
		name, email, password string
		valid                 bool
	}{
		{"minimum", "a@example.com", strings.Repeat("a", 8), true},
		{"unicode minimum", "a@example.com", strings.Repeat("🔑", 8), true},
		{"unicode short", "a@example.com", strings.Repeat("🔑", 7), false},
		{"maximum", "a@example.com", strings.Repeat("a", 128), true},
		{"unicode max bytes", "a@example.com", strings.Repeat("🔑", 128), true},
		{"short", "a@example.com", strings.Repeat("a", 7), false},
		{"long", "a@example.com", strings.Repeat("a", 129), false},
		{"too many bytes", "a@example.com", strings.Repeat("🔑", 129), false},
		{"blank", "a@example.com", strings.Repeat(" ", 8), false},
		{"invalid utf8", "a@example.com", strings.Repeat("a", 15) + "\xff", false},
		{"empty email", "", "a long passphrase", false},
		{"display name", "Alice <a@example.com>", "a long passphrase", false},
		{"unicode email", "用户@example.com", "a long passphrase", false},
		{"invalid domain", "a@-example.com", "a long passphrase", false},
		{"missing domain", "a@", "a long passphrase", false},
		{"long local", strings.Repeat("a", 65) + "@example.com", "a long passphrase", false},
		{"long email", "a@" + strings.Repeat("b", 250) + ".com", "a long passphrase", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, h := &fakeRepository{}, &fakeHasher{}
			_, err := NewService(r, h).Register(context.Background(), RegisterRequest{Email: tc.email, Password: tc.password})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrInvalidUser) || h.calls != 0 || r.calls != 0 {
				t.Fatalf("validation = %v, hash calls %d, db calls %d", err, h.calls, r.calls)
			}
		})
	}
}

func TestRegisterFailures(t *testing.T) {
	input := RegisterRequest{Email: "a@example.com", Password: "a long passphrase"}
	for _, want := range []error{ErrEmailExists, errors.New("database failure")} {
		_, err := NewService(&fakeRepository{err: want}, &fakeHasher{}).Register(context.Background(), input)
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	want := errors.New("random source failure")
	r := &fakeRepository{}
	if _, err := NewService(r, &fakeHasher{err: want}).Register(context.Background(), input); !errors.Is(err, want) || r.calls != 0 {
		t.Fatalf("hash failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &fakeHasher{}
	if _, err := NewService(r, h).Register(ctx, input); !errors.Is(err, context.Canceled) || h.calls != 0 || r.calls != 0 {
		t.Fatalf("canceled: %v", err)
	}
}
