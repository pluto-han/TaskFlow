// Package session implements revocable server-side Bearer sessions.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/pluto-han/taskflow/internal/user"
)

var ErrUnauthorized = errors.New("unauthorized")

type UserID string

type Principal struct {
	SessionID string
	UserID    UserID
	ExpiresAt time.Time
}

type Repository interface {
	Create(context.Context, string, UserID, [32]byte) (Principal, error)
	FindActive(context.Context, [32]byte) (Principal, error)
	Revoke(context.Context, Principal) error
}

type Passwords interface {
	HashPassword(context.Context, string) (string, error)
	VerifyPassword(context.Context, string, string) (bool, error)
}

type Service struct {
	users     user.Repository
	sessions  Repository
	passwords Passwords
	dummyHash string
}

func NewService(ctx context.Context, users user.Repository, sessions Repository, passwords Passwords) (*Service, error) {
	// Never authorize against this hash. Missing users still pay one Argon2 cost.
	dummy, err := passwords.HashPassword(ctx, "dummy password for missing accounts")
	if err != nil {
		return nil, err
	}
	return &Service{users: users, sessions: sessions, passwords: passwords, dummyHash: dummy}, nil
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (s *Service) Login(ctx context.Context, input LoginRequest) (LoginResponse, error) {
	if err := ctx.Err(); err != nil {
		return LoginResponse{}, err
	}
	email, valid := user.NormalizeEmail(input.Email)
	if !valid || len(input.Password) == 0 || len(input.Password) > 512 || !utf8.ValidString(input.Password) {
		return LoginResponse{}, ErrUnauthorized
	}
	u, err := s.users.GetByEmail(ctx, email)
	missing := errors.Is(err, user.ErrUserNotFound)
	if err != nil && !missing {
		return LoginResponse{}, err
	}
	hash := s.dummyHash
	if !missing {
		hash = u.PasswordHash
	}
	ok, err := s.passwords.VerifyPassword(ctx, input.Password, hash)
	if err != nil {
		return LoginResponse{}, err
	}
	if !ok || missing {
		return LoginResponse{}, ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return LoginResponse{}, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return LoginResponse{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(token[:])
	// Hash the canonical token bytes sent in Authorization, not a low-entropy password.
	digest := sha256.Sum256([]byte(encoded))
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return LoginResponse{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	principal, err := s.sessions.Create(ctx, uuid, UserID(u.ID), digest)
	if err != nil {
		return LoginResponse{}, err
	}
	return LoginResponse{AccessToken: encoded, TokenType: "Bearer", ExpiresAt: principal.ExpiresAt}, nil
}

func tokenDigest(token string) ([32]byte, error) {
	if len(token) != 43 {
		return [32]byte{}, ErrUnauthorized
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 {
		return [32]byte{}, ErrUnauthorized
	}
	return sha256.Sum256([]byte(token)), nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return Principal{}, err
	}
	return s.sessions.FindActive(ctx, digest)
}

func (s *Service) Me(ctx context.Context, p Principal) (user.UserResponse, error) {
	u, err := s.users.GetByID(ctx, string(p.UserID))
	if errors.Is(err, user.ErrUserNotFound) {
		return user.UserResponse{}, ErrUnauthorized
	}
	if err != nil {
		return user.UserResponse{}, err
	}
	return user.UserResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}, nil
}

func (s *Service) Logout(ctx context.Context, p Principal) error { return s.sessions.Revoke(ctx, p) }

type principalKey struct{}

// CurrentPrincipal reads the authenticated identity from a typed context key.
func CurrentPrincipal(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
