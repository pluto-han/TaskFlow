package project

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pluto-han/taskflow/internal/session"
)

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func check(ctx context.Context, owner session.UserID) error {
	if owner == "" {
		return session.ErrUnauthorized
	}
	return ctx.Err()
}
func validName(name string) bool {
	return utf8.ValidString(name) && len(strings.TrimSpace(name)) > 0 && utf8.RuneCountInString(name) <= 100 && !strings.ContainsRune(name, 0)
}
func validDescription(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 2000 && !strings.ContainsRune(value, 0)
}

func (s *Service) Create(ctx context.Context, owner session.UserID, input CreateRequest) (*Project, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validName(input.Name) || !validDescription(input.Description) {
		return nil, ErrInvalid
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	return s.repo.Create(ctx, owner, &Project{ID: uuid, OwnerID: owner, Name: input.Name, Description: input.Description})
}
func (s *Service) GetByID(ctx context.Context, owner session.UserID, rawID string) (*Project, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}
	id, err := ParseID(rawID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, owner, id)
}
func (s *Service) List(ctx context.Context, owner session.UserID) ([]Project, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, owner)
}
func (s *Service) Update(ctx context.Context, owner session.UserID, rawID string, input UpdateRequest) (*Project, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}
	id, err := ParseID(rawID)
	if err != nil {
		return nil, err
	}
	if input.Name == nil && input.Description == nil {
		return nil, ErrInvalid
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if !validName(value) {
			return nil, ErrInvalid
		}
		input.Name = &value
	}
	if input.Description != nil && !validDescription(*input.Description) {
		return nil, ErrInvalid
	}
	return s.repo.Update(ctx, owner, id, input)
}
func (s *Service) Delete(ctx context.Context, owner session.UserID, rawID string) error {
	if err := check(ctx, owner); err != nil {
		return err
	}
	id, err := ParseID(rawID)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, owner, id)
}
