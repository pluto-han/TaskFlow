// Package applicationsession implements revocable server-side Bearer and browser sessions.
package applicationsession

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"unicode/utf8"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

type Passwords interface {
	applicationaccount.PasswordHasher
	VerifyPassword(context.Context, string, string) (bool, error)
}

type Service struct {
	users     domainaccount.Repository
	sessions  domainsession.Repository
	passwords Passwords
	dummyHash string
}

func New(
	ctx context.Context,
	users domainaccount.Repository,
	sessions domainsession.Repository,
	passwords Passwords,
) (*Service, error) {
	// Never authorize against this hash. Missing users still pay one Argon2 cost.
	dummy, err := passwords.HashPassword(ctx, "dummy password for missing accounts")
	if err != nil {
		return nil, err
	}

	return &Service{users: users, sessions: sessions, passwords: passwords, dummyHash: dummy}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	result, _, err := s.login(ctx, input, nil)

	return result, err
}

// The credential is returned separately so it cannot be serialized in the DTO.
func (s *Service) LoginBrowser(
	ctx context.Context,
	input LoginInput,
) (BrowserResult, string, error) {
	var csrf [32]byte
	if _, err := rand.Read(csrf[:]); err != nil {
		return BrowserResult{}, "", err
	}

	result, u, err := s.login(ctx, input, &csrf)
	if err != nil {
		return BrowserResult{}, "", err
	}

	return BrowserResult{User: u, ExpiresAt: result.ExpiresAt,
		CSRFToken: base64.RawURLEncoding.EncodeToString(csrf[:])}, result.AccessToken, nil
}

func (s *Service) login(
	ctx context.Context,
	input LoginInput,
	csrf *[32]byte,
) (LoginResult, Profile, error) {
	if err := ctx.Err(); err != nil {
		return LoginResult{}, Profile{}, err
	}

	email, valid := domainaccount.NormalizeEmail(input.Email)
	if !valid || len(input.Password) == 0 || len(input.Password) > 512 || !utf8.ValidString(input.Password) {
		return LoginResult{}, Profile{}, domainaccount.ErrUnauthorized
	}

	u, err := s.users.GetByEmail(ctx, email)
	missing := errors.Is(err, domainaccount.ErrUserNotFound)
	if err != nil && !missing {
		return LoginResult{}, Profile{}, err
	}

	hash := s.dummyHash
	if !missing {
		hash = u.PasswordHash
	}
	ok, err := s.passwords.VerifyPassword(ctx, input.Password, hash)
	if err != nil {
		return LoginResult{}, Profile{}, err
	}
	if !ok || missing {
		return LoginResult{}, Profile{}, domainaccount.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return LoginResult{}, Profile{}, err
	}

	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return LoginResult{}, Profile{}, err
	}

	encoded := base64.RawURLEncoding.EncodeToString(token[:])
	// Hash the canonical token bytes sent in Authorization, not a low-entropy password.
	digest := sha256.Sum256([]byte(encoded))
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return LoginResult{}, Profile{}, err
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	var principal domainsession.Principal
	if csrf == nil {
		principal, err = s.sessions.Create(ctx, uuid, domainaccount.UserID(u.ID), digest, u.CredentialsVersion)
	} else {
		principal, err = s.sessions.CreateBrowser(ctx, uuid, domainaccount.UserID(u.ID), digest, *csrf, u.CredentialsVersion)
	}
	if err != nil {
		return LoginResult{}, Profile{}, err
	}

	return LoginResult{AccessToken: encoded, TokenType: "Bearer", ExpiresAt: principal.ExpiresAt},
		Profile{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}, nil
}

func TokenDigest(token string) ([32]byte, error) {
	if len(token) != 43 {
		return [32]byte{}, domainaccount.ErrUnauthorized
	}

	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 {
		return [32]byte{}, domainaccount.ErrUnauthorized
	}

	return sha256.Sum256([]byte(token)), nil
}

func (s *Service) Authenticate(
	ctx context.Context,
	token string,
) (domainsession.Principal, error) {
	if err := ctx.Err(); err != nil {
		return domainsession.Principal{}, err
	}

	digest, err := TokenDigest(token)
	if err != nil {
		return domainsession.Principal{}, err
	}

	return s.sessions.FindActive(ctx, digest)
}

func (s *Service) AuthenticateBrowser(
	ctx context.Context,
	token string,
) (domainsession.BrowserPrincipal, error) {
	if err := ctx.Err(); err != nil {
		return domainsession.BrowserPrincipal{}, err
	}

	digest, err := TokenDigest(token)
	if err != nil {
		return domainsession.BrowserPrincipal{}, err
	}

	return s.sessions.FindBrowser(ctx, digest)
}

func (s *Service) Me(ctx context.Context, p domainsession.Principal) (Profile, error) {
	u, err := s.users.GetByID(ctx, string(p.UserID))
	if errors.Is(err, domainaccount.ErrUserNotFound) {
		return Profile{}, domainaccount.ErrUnauthorized
	}
	if err != nil {
		return Profile{}, err
	}

	return Profile{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}, nil
}

func (s *Service) Logout(ctx context.Context, p domainsession.Principal) error {
	return s.sessions.Revoke(ctx, p)
}
