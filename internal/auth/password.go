// Package auth contains password primitives only; it does not authenticate requests.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidHash     = errors.New("invalid password hash")
	ErrInvalidPassword = errors.New("invalid password length")
	ErrBusy            = errors.New("password workers busy")
)

const (
	memory      = 64 * 1024 // KiB, per hash
	iterations  = 3
	parallelism = 4
	saltLength  = 16
	keyLength   = 32
	prefix      = "$argon2id$v=19$m=65536,t=3,p=4$"
)

// A process-wide limit bounds active hashing to 128 MiB, plus runtime overhead.
// Reject excess work instead of accumulating unbounded waiting requests.
var workers = make(chan struct{}, 2)

type PasswordHasher struct{}

func acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case workers <- struct{}{}:
		return nil
	default:
		return ErrBusy
	}
}

func (PasswordHasher) HashPassword(ctx context.Context, password string) (string, error) {
	if len(password) == 0 || len(password) > 512 {
		return "", ErrInvalidPassword
	}
	if err := acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-workers }()
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return prefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func (PasswordHasher) VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	// Accept only this supported profile. Do not parse untrusted integers and pass
	// them to Argon2: arbitrary memory/time/parallelism values permit resource abuse.
	if len(encoded) != len(prefix)+22+1+43 || !strings.HasPrefix(encoded, prefix) {
		return false, ErrInvalidHash
	}
	parts := strings.Split(strings.TrimPrefix(encoded, prefix), "$")
	if len(parts) != 2 {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(salt) != saltLength {
		return false, ErrInvalidHash
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(expected) != keyLength {
		return false, ErrInvalidHash
	}
	if len(password) == 0 || len(password) > 512 {
		return false, ErrInvalidPassword
	}
	if err := acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-workers }()
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
