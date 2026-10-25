package task

import "context"

type Service struct {
	repo Repoisitory
}

func NewService(repo Repoisitory) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetByID(ctx context.Context, id string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindByID(id)
}

func (s *Service) List(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.repo.FindAll()
}
