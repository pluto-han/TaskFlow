// Package session implements revocable server-side Bearer and browser sessions.
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

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

var ErrUnauthorized = domainaccount.ErrUnauthorized

type UserID = domainaccount.UserID

type Principal struct {
	SessionID string
	UserID    UserID
	ExpiresAt time.Time
}

type BrowserPrincipal struct {
	Principal
	CSRFSecret [32]byte
}

type Repository interface {
	Create(context.Context, string, UserID, [32]byte, int64) (Principal, error)
	FindActive(context.Context, [32]byte) (Principal, error)
	CreateBrowser(context.Context, string, UserID, [32]byte, [32]byte, int64) (Principal, error)
	FindBrowser(context.Context, [32]byte) (BrowserPrincipal, error)
	Revoke(context.Context, Principal) error
	ListActive(context.Context, UserID, ListQuery) ([]SessionInfo, error)
	RevokeOwned(context.Context, Principal, string) error
	ChangePassword(context.Context, Principal, int64, string) error
}

type Passwords interface {
	HashPassword(context.Context, string) (string, error)
	VerifyPassword(context.Context, string, string) (bool, error)
}

type Service struct {
	users     domainaccount.Repository
	sessions  Repository
	passwords Passwords
	dummyHash string
}

func NewService(ctx context.Context, users domainaccount.Repository, sessions Repository, passwords Passwords) (*Service, error) {
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
	result, _, err := s.login(ctx, input, nil)
	return result, err
}

type BrowserResponse struct {
	User      UserResponse `json:"user"`
	ExpiresAt time.Time    `json:"expires_at"`
	CSRFToken string       `json:"csrf_token"`
}

// The credential is returned separately so it cannot be serialized in the DTO.
func (s *Service) LoginBrowser(ctx context.Context, input LoginRequest) (BrowserResponse, string, error) {
	var csrf [32]byte
	if _, err := rand.Read(csrf[:]); err != nil {
		return BrowserResponse{}, "", err
	}
	result, u, err := s.login(ctx, input, &csrf)
	if err != nil {
		return BrowserResponse{}, "", err
	}
	return BrowserResponse{User: u, ExpiresAt: result.ExpiresAt,
		CSRFToken: base64.RawURLEncoding.EncodeToString(csrf[:])}, result.AccessToken, nil
}

func (s *Service) login(ctx context.Context, input LoginRequest, csrf *[32]byte) (LoginResponse, UserResponse, error) {
	if err := ctx.Err(); err != nil {
		return LoginResponse{}, UserResponse{}, err
	}
	email, valid := domainaccount.NormalizeEmail(input.Email)
	if !valid || len(input.Password) == 0 || len(input.Password) > 512 || !utf8.ValidString(input.Password) {
		return LoginResponse{}, UserResponse{}, ErrUnauthorized
	}
	u, err := s.users.GetByEmail(ctx, email)
	missing := errors.Is(err, domainaccount.ErrUserNotFound)
	if err != nil && !missing {
		return LoginResponse{}, UserResponse{}, err
	}
	hash := s.dummyHash
	if !missing {
		hash = u.PasswordHash
	}
	ok, err := s.passwords.VerifyPassword(ctx, input.Password, hash)
	if err != nil {
		return LoginResponse{}, UserResponse{}, err
	}
	if !ok || missing {
		return LoginResponse{}, UserResponse{}, ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return LoginResponse{}, UserResponse{}, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return LoginResponse{}, UserResponse{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(token[:])
	// Hash the canonical token bytes sent in Authorization, not a low-entropy password.
	digest := sha256.Sum256([]byte(encoded))
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return LoginResponse{}, UserResponse{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	var principal Principal
	if csrf == nil {
		principal, err = s.sessions.Create(ctx, uuid, UserID(u.ID), digest, u.CredentialsVersion)
	} else {
		principal, err = s.sessions.CreateBrowser(ctx, uuid, UserID(u.ID), digest, *csrf, u.CredentialsVersion)
	}
	if err != nil {
		return LoginResponse{}, UserResponse{}, err
	}
	return LoginResponse{AccessToken: encoded, TokenType: "Bearer", ExpiresAt: principal.ExpiresAt},
		UserResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}, nil
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

func (s *Service) AuthenticateBrowser(ctx context.Context, token string) (BrowserPrincipal, error) {
	if err := ctx.Err(); err != nil {
		return BrowserPrincipal{}, err
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return BrowserPrincipal{}, err
	}
	return s.sessions.FindBrowser(ctx, digest)
}

func (s *Service) Me(ctx context.Context, p Principal) (UserResponse, error) {
	u, err := s.users.GetByID(ctx, string(p.UserID))
	if errors.Is(err, domainaccount.ErrUserNotFound) {
		return UserResponse{}, ErrUnauthorized
	}
	if err != nil {
		return UserResponse{}, err
	}
	return UserResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}, nil
}

func (s *Service) Logout(ctx context.Context, p Principal) error { return s.sessions.Revoke(ctx, p) }

type principalKey struct{}

// CurrentPrincipal reads the authenticated identity from a typed context key.
func CurrentPrincipal(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
