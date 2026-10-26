package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPasswordRoundTripAndRandomSalt(t *testing.T) {
	h := PasswordHasher{}
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
	for _, encoded := range []string{first, second} {
		for _, tc := range []struct {
			password string
			want     bool
		}{{password, true}, {"wrong long password", false}} {
			ok, err := h.VerifyPassword(ctx, tc.password, encoded)
			if err != nil || ok != tc.want {
				t.Fatalf("verify = %v, %v; want %v", ok, err, tc.want)
			}
		}
	}
}

func TestRejectMalformedHashBeforeWork(t *testing.T) {
	// Saturate workers: malformed input must still return InvalidHash, not Busy.
	workers <- struct{}{}
	workers <- struct{}{}
	defer func() { <-workers; <-workers }()
	validShape := prefix + strings.Repeat("A", 22) + "$" + strings.Repeat("A", 43)
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
		prefix + strings.Repeat("!", 22) + "$" + strings.Repeat("A", 43),
		prefix + strings.Repeat("A", 21) + "B$" + strings.Repeat("A", 43), // noncanonical base64
		prefix + strings.Repeat("A", 22) + "$" + strings.Repeat("!", 43),
		prefix + strings.Repeat("A", 21) + "$" + strings.Repeat("A", 44), // wrong salt/key lengths
	}
	for i, encoded := range cases {
		ok, err := (PasswordHasher{}).VerifyPassword(context.Background(), "password", encoded)
		if ok || !errors.Is(err, ErrInvalidHash) {
			t.Fatalf("case %d: %v, %v", i, ok, err)
		}
	}
	_, err := (PasswordHasher{}).HashPassword(context.Background(), "a long password")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("saturated workers: %v", err)
	}
}

func TestPasswordCancellationAndLength(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PasswordHasher{}).HashPassword(ctx, "a long password"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, password := range []string{"", strings.Repeat("a", 513)} {
		if _, err := (PasswordHasher{}).HashPassword(context.Background(), password); !errors.Is(err, ErrInvalidPassword) {
			t.Fatal(err)
		}
	}
	if len(workers) != 0 {
		t.Fatal("worker leaked")
	}
}

func BenchmarkHashPassword(b *testing.B) {
	for b.Loop() {
		if _, err := (PasswordHasher{}).HashPassword(context.Background(), "a long benchmark passphrase"); err != nil {
			b.Fatal(err)
		}
	}
}
