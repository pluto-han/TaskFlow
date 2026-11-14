package applicationproject

import (
	"context"
	"crypto/rand"
	"fmt"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

type Service struct {
	repo   domainproject.Repository
	shares domainproject.SharingRepository
}

func New(repo domainproject.Repository, shares domainproject.SharingRepository) *Service {
	return &Service{repo: repo, shares: shares}
}

func check(ctx context.Context, owner domainaccount.UserID) error {
	if owner == "" {
		return domainaccount.ErrUnauthorized
	}

	return ctx.Err()
}

func (s *Service) Create(
	ctx context.Context,
	owner domainaccount.UserID,
	input CreateInput,
) (*ProjectResult, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}

	project, err := domainproject.NewProject(owner, input.Name, input.Description)
	if err != nil {
		return nil, err
	}

	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	project.ID = uuid
	saved, err := s.repo.Create(ctx, owner, project)
	if err != nil {
		return nil, err
	}

	return projectResult(saved, owner), nil
}

func (s *Service) GetByID(
	ctx context.Context,
	owner domainaccount.UserID,
	rawID string,
) (*ProjectResult, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}

	id, err := domainproject.ParseID(rawID)
	if err != nil {
		return nil, err
	}

	saved, err := s.repo.GetByID(ctx, owner, id)
	if err != nil {
		return nil, err
	}

	return projectResult(saved, owner), nil
}

func (s *Service) List(ctx context.Context, owner domainaccount.UserID) ([]ProjectResult, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}

	rows, err := s.repo.List(ctx, owner)
	if err != nil {
		return nil, err
	}

	result := make([]ProjectResult, len(rows))
	for i := range rows {
		result[i] = *projectResult(&rows[i], owner)
	}

	return result, nil
}

func (s *Service) Update(
	ctx context.Context,
	owner domainaccount.UserID,
	rawID string,
	input domainproject.Change,
) (*ProjectResult, error) {
	if err := check(ctx, owner); err != nil {
		return nil, err
	}

	id, err := domainproject.ParseID(rawID)
	if err != nil {
		return nil, err
	}

	input, err = input.Normalize()
	if err != nil {
		return nil, err
	}

	saved, err := s.repo.Update(ctx, owner, id, input)
	if err != nil {
		return nil, err
	}

	return projectResult(saved, owner), nil
}

func (s *Service) Delete(ctx context.Context, owner domainaccount.UserID, rawID string) error {
	if err := check(ctx, owner); err != nil {
		return err
	}

	id, err := domainproject.ParseID(rawID)
	if err != nil {
		return err
	}

	return s.repo.Delete(ctx, owner, id)
}
