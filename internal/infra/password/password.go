// Package infrapassword contains password primitives only; it does not authenticate requests.
package infrapassword

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidHash     = errors.New("invalid password hash")
	ErrInvalidPassword = errors.New("invalid password length")
	ErrBusy            = applicationaccount.ErrPasswordBusy
)

const (
	_memory      = 64 * 1024 // KiB, per hash
	_iterations  = 3
	_parallelism = 4
	_saltLength  = 16
	_keyLength   = 32
	_prefix      = "$argon2id$v=19$m=65536,t=3,p=4$"
)

// A process-wide limit bounds active hashing to 128 MiB, plus runtime overhead.
// Reject excess work instead of accumulating unbounded waiting requests.
var _workers = make(chan struct{}, 2)

type PasswordHasher struct{}

func (PasswordHasher) HashPassword(ctx context.Context, password string) (string, error) {
	if len(password) == 0 || len(password) > 512 {
		return "", ErrInvalidPassword
	}

	if err := acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-_workers }()
	salt := make([]byte, _saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := argon2.IDKey([]byte(password), salt, _iterations, _memory, _parallelism, _keyLength)
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return _prefix +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(key), nil
}

func (PasswordHasher) VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	// Accept only this supported profile. Do not parse untrusted integers and pass
	// them to Argon2: arbitrary memory/time/parallelism values permit resource abuse.
	if len(encoded) != len(_prefix)+22+1+43 || !strings.HasPrefix(encoded, _prefix) {
		return false, ErrInvalidHash
	}

	parts := strings.Split(strings.TrimPrefix(encoded, _prefix), "$")
	if len(parts) != 2 {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(salt) != _saltLength {
		return false, ErrInvalidHash
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(expected) != _keyLength {
		return false, ErrInvalidHash
	}

	if len(password) == 0 || len(password) > 512 {
		return false, ErrInvalidPassword
	}

	if err := acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-_workers }()

	actual := argon2.IDKey([]byte(password), salt, _iterations, _memory, _parallelism, _keyLength)
	if err := ctx.Err(); err != nil {
		return false, err
	}

	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case _workers <- struct{}{}:
		return nil
	default:
		return ErrBusy
	}
}
