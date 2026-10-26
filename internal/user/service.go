package user

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type PasswordHasher interface {
	HashPassword(context.Context, string) (string, error)
}

type Service struct {
	repo      Repository
	passwords PasswordHasher
}

func NewService(repo Repository, passwords PasswordHasher) *Service {
	return &Service{repo: repo, passwords: passwords}
}

func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) < 3 || len(email) > 254 {
		return "", false
	}
	for _, c := range email {
		if c <= 32 || c >= 127 {
			return "", false
		}
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" {
		return "", false
	}
	// Restrict this lesson to common, unquoted ASCII mailbox addresses.
	local, domain, ok := strings.Cut(email, "@")
	if !ok || len(local) > 64 || strings.ContainsAny(local, "\"\\") || !strings.Contains(domain, ".") {
		return "", false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}
	return email, true
}

func (s *Service) Register(ctx context.Context, input RegisterRequest) (*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	email, valid := normalizeEmail(input.Email)
	count := utf8.RuneCountInString(input.Password)
	if !valid || !utf8.ValidString(input.Password) || count < 15 || count > 128 || len(input.Password) > 512 || strings.TrimSpace(input.Password) == "" {
		return nil, ErrInvalidUser
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
	return s.repo.Create(ctx, &User{ID: uuid, Email: email, PasswordHash: hash})
}
