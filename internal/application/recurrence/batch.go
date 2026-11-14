package applicationrecurrence

import (
	"context"
	"errors"
	"log/slog"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

const BatchSize = 20

const BatchTimeout = 5 * time.Second

var ErrDayChanged = errors.New("planning date changed during scan")

type BatchResult struct {
	Processed int
	Generated int
	Affected  []AffectedPlan
}

type BatchRunner interface {
	GenerateBatch(context.Context, func() time.Time) (BatchResult, error)
}

type AffectedPlan struct {
	Owner domainaccount.UserID
	Date  string
}

type Generator struct {
	repo       BatchRunner
	invalidate func(context.Context, domainaccount.UserID, string) error
}

func NewGenerator(
	repo BatchRunner,
	invalidate func(context.Context, domainaccount.UserID, string) error,
) *Generator {
	return &Generator{repo: repo, invalidate: invalidate}
}

func (g *Generator) GenerateBatch(ctx context.Context, now func() time.Time) (BatchResult, error) {
	result, err := g.repo.GenerateBatch(ctx, now)
	if err != nil {
		return BatchResult{}, err
	}

	failed := 0
	if g.invalidate != nil {
		for _, plan := range result.Affected {
			if err := g.invalidate(ctx, plan.Owner, plan.Date); err != nil {
				failed++
			}
		}
	}
	if failed > 0 {
		slog.Warn("recurrence_cache_invalidation_failed", "failed", failed)
	}
	return result, nil
}
