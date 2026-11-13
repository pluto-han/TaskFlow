package applicationaccount

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

// ErrPasswordBusy indicates that the bounded password workers are saturated.
var ErrPasswordBusy = errors.New("password workers busy")

type PasswordHasher interface {
	HashPassword(context.Context, string) (string, error)
}

type Service struct {
	repo      domainaccount.Repository
	passwords PasswordHasher
}

func New(repo domainaccount.Repository, passwords PasswordHasher) *Service {
	return &Service{repo: repo, passwords: passwords}
}

func (s *Service) Register(ctx context.Context, input CreateInput) (*Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	user, err := domainaccount.NewUser(input.Email, input.Password)
	if err != nil {
		return nil, err
	}

	hash, err := s.passwords.HashPassword(ctx, input.Password)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])

	user.ID, user.PasswordHash = uuid, hash
	saved, err := s.repo.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	return profileFromUser(saved), nil
}
