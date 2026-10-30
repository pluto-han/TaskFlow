package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/session"
)

type PostgresRepository struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{queries: db.New(pool), pool: pool}
}
func ownerUUID(raw session.UserID) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(string(raw)); err != nil || !id.Valid {
		return id, session.ErrUnauthorized
	}
	return id, nil
}
func idUUID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil || !id.Valid {
		return id, ErrInvalid
	}
	return id, nil
}
func databaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && (pgErr.ConstraintName == "tasks_project_owner_fk" || pgErr.ConstraintName == "tasks_project_fk") {
		return ErrNotEmpty
	}
	return fmt.Errorf("project repository: %w", err)
}
func fromRow(row db.Project, err error) (*Project, error) {
	if err != nil {
		return nil, databaseError(err)
	}
	if !row.ID.Valid || !row.OwnerID.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid || row.CreatedAt.InfinityModifier != pgtype.Finite || row.UpdatedAt.InfinityModifier != pgtype.Finite {
		return nil, errors.New("invalid project mapping")
	}
	return &Project{ID: row.ID.String(), OwnerID: session.UserID(row.OwnerID.String()), Name: row.Name, Description: row.Description, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC()}, nil
}
func (r *PostgresRepository) Create(ctx context.Context, owner session.UserID, p *Project) (*Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}
	id, err := idUUID(p.ID)
	if err != nil {
		return nil, err
	}
	result, err := fromRow(r.queries.CreateProject(ctx, db.CreateProjectParams{ID: id, OwnerID: uid, Name: p.Name, Description: p.Description}))
	if err == nil {
		result.Role = "owner"
	}
	return result, err
}
func (r *PostgresRepository) GetByID(ctx context.Context, owner session.UserID, rawID string) (*Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}
	id, err := idUUID(rawID)
	if err != nil {
		return nil, err
	}
	result, err := fromRow(r.queries.GetProject(ctx, db.GetProjectParams{ID: id, OwnerID: uid}))
	if err == nil {
		result.Role = "member"
		if result.OwnerID == owner {
			result.Role = "owner"
		}
	}
	return result, err
}
func (r *PostgresRepository) List(ctx context.Context, owner session.UserID) ([]Project, error) {
	uid, err := ownerUUID(owner)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListProjects(ctx, uid)
	if err != nil {
		return nil, databaseError(err)
	}
	result := make([]Project, 0, len(rows))
	for _, row := range rows {
		p, err := fromRow(row, nil)
		if err != nil {
			return nil, err
		}
		p.Role = "member"
		if p.OwnerID == owner {
			p.Role = "owner"
		}
		result = append(result, *p)
	}
	return result, nil
}
func (r *PostgresRepository) Update(ctx context.Context, owner session.UserID, rawID string, input UpdateRequest) (*Project, error) {
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
	var result *Project
	err = r.withAccess(ctx, owner, rawID, true, func(tx pgx.Tx) error {
		var e error
		result, e = fromRow(r.queries.WithTx(tx).UpdateProject(ctx, params))
		return e
	})
	if err == nil {
		result.Role = "owner"
	}
	return result, err
}
func (r *PostgresRepository) Delete(ctx context.Context, owner session.UserID, rawID string) error {
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

var _ Repository = (*PostgresRepository)(nil)
