package password_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
)

// The supported persisted hash profile is a compatibility contract.
const _prefix = "$argon2id$v=19$m=65536,t=3,p=4$"

func TestPasswordRoundTripAndRandomSalt(t *testing.T) {
	h := infrapassword.PasswordHasher{}
	ctx := context.Background()
	password := "a long test passphrase"

	first, err := h.HashPassword(ctx, password)
	if err != nil {
		t.Fatal(err)
	}

	second, err := h.HashPassword(ctx, password)
	if err != nil {
		t.Fatal(err)
	}

	if first == second || strings.Contains(first, password) {
		t.Fatal("hash must use a fresh salt and not contain plaintext")
	}

	tests := []string{first, second}

	for _, encoded := range tests {
		tests := []struct {
			password string
			want     bool
		}{{password, true}, {"wrong long password", false}}

		for _, tt := range tests {
			ok, err := h.VerifyPassword(ctx, tt.password, encoded)
			if err != nil || ok != tt.want {
				t.Fatalf("verify = %v, %v; want %v", ok, err, tt.want)
			}
		}
	}
}

func TestRejectMalformedHashBeforeWork(t *testing.T) {
	// Saturate workers: malformed input must still return InvalidHash, not Busy.
	holdPasswordWorkers(t)

	validShape := _prefix + strings.Repeat("A", 22) + "$" + strings.Repeat("A", 43)
	cases := []string{
		"", "broken", strings.Repeat("x", 10000), validShape + "$extra",
		strings.Replace(validShape, "argon2id", "argon2i", 1),
		strings.Replace(validShape, "v=19", "v=18", 1),
		strings.Replace(validShape, "m=65536", "m=0", 1),
		strings.Replace(validShape, "m=65536", "m=999999999999999999999", 1),
		strings.Replace(validShape, "t=3", "t=0", 1),
		strings.Replace(validShape, "t=3", "t=999999999", 1),
		strings.Replace(validShape, "p=4", "p=0", 1),
		strings.Replace(validShape, "p=4", "p=255", 1),
		_prefix + strings.Repeat("!", 22) + "$" + strings.Repeat("A", 43),
		_prefix + strings.Repeat("A", 21) + "B$" + strings.Repeat("A", 43), // noncanonical base64
		_prefix + strings.Repeat("A", 22) + "$" + strings.Repeat("!", 43),
		_prefix + strings.Repeat("A", 21) + "$" + strings.Repeat("A", 44), // wrong salt/key lengths
	}

	for i, encoded := range cases {
		ok, err := (infrapassword.PasswordHasher{}).VerifyPassword(
			context.Background(),
			"password",
			encoded,
		)
		if ok || !errors.Is(err, infrapassword.ErrInvalidHash) {
			t.Fatalf("case %d: %v, %v", i, ok, err)
		}
	}

	_, err := (infrapassword.PasswordHasher{}).HashPassword(context.Background(), "a long password")
	if !errors.Is(err, infrapassword.ErrBusy) {
		t.Fatalf("saturated workers: %v", err)
	}
}

func TestPasswordCancellationAndLength(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (infrapassword.PasswordHasher{}).HashPassword(ctx, "a long password"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	tests := []string{"", strings.Repeat("a", 513)}

	for _, password := range tests {
		if _, err := (infrapassword.PasswordHasher{}).HashPassword(context.Background(), password); !errors.Is(err, infrapassword.ErrInvalidPassword) {
			t.Fatal(err)
		}
	}

	// Both slots must remain usable after canceled/invalid requests.
	holdPasswordWorkers(t)
}

func BenchmarkHashPassword(b *testing.B) {
	for b.Loop() {
		if _, err := (infrapassword.PasswordHasher{}).HashPassword(
			context.Background(),
			"a long benchmark passphrase",
		); err != nil {
			b.Fatal(err)
		}
	}
}

// HashPassword checks cancellation after hashing while still holding its slot.
// Pausing that check lets tests observe saturation through the public API.
type heldHashContext struct {
	context.Context

	checks  int
	entered chan struct{}
	release <-chan struct{}
}

func (c *heldHashContext) Err() error {
	c.checks++
	if c.checks == 2 {
		close(c.entered)
		<-c.release
	}

	return nil
}

func holdPasswordWorkers(t *testing.T) {
	t.Helper()

	release := make(chan struct{})
	done := make(chan error, 2)

	t.Cleanup(func() {
		close(release)
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Error("password worker did not finish")
			}
		}
	})

	contexts := make([]*heldHashContext, 2)
	for i := range contexts {
		contexts[i] = &heldHashContext{
			Context: context.Background(),
			entered: make(chan struct{}),
			release: release,
		}
		go func(ctx context.Context) {
			_, err := (infrapassword.PasswordHasher{}).HashPassword(ctx, "a long held password")
			done <- err
		}(contexts[i])
	}

	for _, ctx := range contexts {
		select {
		case <-ctx.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("password slot unavailable")
		}
	}
}
