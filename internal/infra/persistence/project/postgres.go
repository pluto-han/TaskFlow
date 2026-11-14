package infraproject

import (
	"context"
	"errors"
	"fmt"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

type Repository struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: db.New(pool), pool: pool}
}

func ownerUUID(raw domainaccount.UserID) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(string(raw)); err != nil || !id.Valid {
		return id, domainaccount.ErrUnauthorized
	}

	return id, nil
}

func idUUID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil || !id.Valid {
		return id, domainproject.ErrInvalid
	}

	return id, nil
}

func databaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domainproject.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && (pgErr.ConstraintName == "tasks_project_owner_fk" || pgErr.ConstraintName == "tasks_project_fk") {
		return domainproject.ErrNotEmpty
	}

	return fmt.Errorf("project repository: %w", err)
}

func (r *Repository) Create(
	ctx context.Context,
	owner domainaccount.UserID,
	p *domainproject.Project,
) (*domainproject.Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}

	id, err := idUUID(p.ID)
	if err != nil {
		return nil, err
	}

	result, err := fromRow(r.queries.CreateProject(ctx, db.CreateProjectParams{
		ID:          id,
		OwnerID:     uid,
		Name:        p.Name,
		Description: p.Description,
	}))

	return result, err
}

func (r *Repository) GetByID(
	ctx context.Context,
	owner domainaccount.UserID,
	rawID string,
) (*domainproject.Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}

	id, err := idUUID(rawID)
	if err != nil {
		return nil, err
	}

	result, err := fromRow(r.queries.GetProject(ctx, db.GetProjectParams{ID: id, OwnerID: uid}))

	return result, err
}

func (r *Repository) List(
	ctx context.Context,
	owner domainaccount.UserID,
) ([]domainproject.Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}

	rows, err := r.queries.ListProjects(ctx, uid)
	if err != nil {
		return nil, databaseError(err)
	}

	result := make([]domainproject.Project, 0, len(rows))
	for _, row := range rows {
		p, err := fromRow(row, nil)
		if err != nil {
			return nil, err
		}

		result = append(result, *p)
	}

	return result, nil
}

func (r *Repository) Update(
	ctx context.Context,
	owner domainaccount.UserID,
	rawID string,
	input domainproject.Change,
) (*domainproject.Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}

	id, err := idUUID(rawID)
	if err != nil {
		return nil, err
	}

	params := db.UpdateProjectParams{ID: id, OwnerID: uid}
	if input.Name != nil {
		params.Name = pgtype.Text{String: *input.Name, Valid: true}
	}
	if input.Description != nil {
		params.Description = pgtype.Text{String: *input.Description, Valid: true}
	}
	var result *domainproject.Project
	err = r.withAccess(ctx, owner, rawID, true, func(tx pgx.Tx) error {
		var e error
		result, e = fromRow(r.queries.WithTx(tx).UpdateProject(ctx, params))

		return e
	})

	return result, err
}

func (r *Repository) Delete(ctx context.Context, owner domainaccount.UserID, rawID string) error {
	uid, err := ownerUUID(owner)
	if err != nil {
		return err
	}

	id, err := idUUID(rawID)
	if err != nil {
		return err
	}

	return r.withAccess(ctx, owner, rawID, true, func(tx pgx.Tx) error {
		_, e := r.queries.WithTx(tx).DeleteProject(ctx, db.DeleteProjectParams{ID: id, OwnerID: uid})

		return e
	})
}

var _ domainproject.Repository = (*Repository)(nil)
