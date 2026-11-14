// Package applicationintegration owns account-to-channel binding, independently of task delivery.
package applicationintegration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
)

type Service struct {
	repo    domainintegration.Repository
	enabled bool
}

func New(repo domainintegration.Repository, enabled bool) *Service {
	return &Service{repo: repo, enabled: enabled}
}

func (s *Service) Enabled() bool { return s != nil && s.enabled }

func (s *Service) Status(
	ctx context.Context,
	owner domainaccount.UserID,
) (domainintegration.Binding, error) {
	result, err := s.repo.Status(ctx, owner)
	result.Enabled = s.Enabled()

	return result, err
}

func (s *Service) Issue(
	ctx context.Context,
	owner domainaccount.UserID,
) (domainintegration.Code, error) {
	if !s.Enabled() {
		return domainintegration.Code{}, domainintegration.ErrInvalid
	}

	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return domainintegration.Code{}, err
	}

	raw := base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(raw))
	expires, err := s.repo.Issue(ctx, owner, hash[:])
	if err != nil {
		return domainintegration.Code{}, err
	}

	return domainintegration.Code{Code: raw, ExpiresAt: expires}, nil
}

func (s *Service) Bind(
	ctx context.Context,
	code string,
	target domainintegration.Target,
) (bool, error) {
	if !s.Enabled() || !ValidCode(code) {
		return false, domainintegration.ErrCode
	}

	hash := sha256.Sum256([]byte(code))

	return s.repo.Bind(ctx, hash[:], target)
}

func (s *Service) Unbind(ctx context.Context, owner domainaccount.UserID, version int64) error {
	return s.repo.Unbind(ctx, owner, version)
}

func ValidCode(code string) bool {
	if len(code) != 43 {
		return false
	}

	decoded, e := base64.RawURLEncoding.Strict().DecodeString(code)

	return e == nil && len(decoded) == 32
}
