package applicationaccount

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

type CapabilityRepository interface {
	Request(context.Context, domainsession.Principal, []byte, string) error
	Confirm(context.Context, []byte) error
	RequestReset(context.Context, string, []byte, []byte, string) error
	FindReset(context.Context, []byte) (ResetIdentity, error)
	ConfirmReset(context.Context, []byte, ResetIdentity, string) error
	VerifiedAt(context.Context, domainaccount.UserID) (*time.Time, error)
}

type ResetIdentity struct {
	Owner   string
	Version int64
}

type CapabilityService struct {
	repo      CapabilityRepository
	enabled   bool
	origin    string
	passwords PasswordHasher
}

func NewCapabilities(
	repo CapabilityRepository,
	enabled bool,
	origin string,
	passwords PasswordHasher,
) (*CapabilityService, error) {
	if enabled {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
			return nil, errors.New("mail requires a fixed HTTPS or loopback origin")
		}
	}
	return &CapabilityService{repo: repo, enabled: enabled, origin: origin, passwords: passwords}, nil
}

func (s *CapabilityService) Enabled() bool { return s != nil && s.enabled }

func (s *CapabilityService) WithPasswords(p PasswordHasher) *CapabilityService {
	s.passwords = p

	return s
}

// Request issues a verification capability only for the authenticated account.
func (s *CapabilityService) Request(ctx context.Context, p domainsession.Principal) error {
	if !s.Enabled() {
		return domainaccount.ErrDisabled
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	return s.repo.Request(ctx, p, hash[:], s.origin+"/verify-email#token="+token)
}

// Confirm consumes an explicit capability, independent of login state.
func (s *CapabilityService) Confirm(ctx context.Context, raw string) error {
	if !s.Enabled() {
		return domainaccount.ErrDisabled
	}

	hash, err := tokenHash(raw)
	if err != nil {
		return err
	}

	return s.repo.Confirm(ctx, hash)
}

// RequestReset hides missing and throttled targets behind the same successful outcome.
func (s *CapabilityService) RequestReset(ctx context.Context, input ResetInput) error {
	if !s.Enabled() {
		return domainaccount.ErrDisabled
	}

	address, valid := domainaccount.NormalizeEmail(input.Email)
	if !valid {
		return domainaccount.ErrResetEmail
	}

	target := sha256.Sum256([]byte(address))
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	return s.repo.RequestReset(ctx, address, target[:], hash[:], s.origin+"/reset-password#token="+token)
}

func (s *CapabilityService) ConfirmReset(ctx context.Context, input ResetConfirmInput) error {
	if !s.Enabled() {
		return domainaccount.ErrDisabled
	}

	hash, err := tokenHash(input.Token)
	if err != nil {
		return domainaccount.ErrResetToken
	}
	if !domainaccount.ValidPassword(input.NewPassword) {
		return domainaccount.ErrResetPassword
	}

	identity, err := s.repo.FindReset(ctx, hash)
	if err != nil {
		return err
	}

	encoded, err := s.passwords.HashPassword(ctx, input.NewPassword)
	if err != nil {
		return err
	}

	return s.repo.ConfirmReset(ctx, hash, identity, encoded)
}

func tokenHash(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != raw {
		return nil, domainaccount.ErrToken
	}

	hash := sha256.Sum256([]byte(raw))

	return hash[:], nil
}

type ResetInput struct {
	Email string
}

type ResetConfirmInput struct {
	Token       string
	NewPassword string
}

type VerificationResult struct {
	Enabled    bool
	VerifiedAt *time.Time
}

func (s *CapabilityService) Status(
	ctx context.Context,
	p domainsession.Principal,
) (VerificationResult, error) {
	at, err := s.repo.VerifiedAt(ctx, p.UserID)

	return VerificationResult{Enabled: s.Enabled(), VerifiedAt: at}, err
}
