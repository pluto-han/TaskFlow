package infraproject

import (
	"errors"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

func fromRow(row db.Project, err error) (*domainproject.Project, error) {
	if err != nil {
		return nil, databaseError(err)
	}
	if !row.ID.Valid || !row.OwnerID.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid || row.CreatedAt.InfinityModifier != pgtype.Finite || row.UpdatedAt.InfinityModifier != pgtype.Finite {
		return nil, errors.New("invalid project mapping")
	}

	return &domainproject.Project{
		ID:          row.ID.String(),
		OwnerID:     domainaccount.UserID(row.OwnerID.String()),
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}, nil
}

func scanInvitation(row pgx.Row) (*domainproject.Invitation, error) {
	var i domainproject.Invitation
	e := row.Scan(&i.ID, &i.ProjectID, &i.InviteeUserID, &i.CreatedAt, &i.ExpiresAt, &i.RevokedAt, &i.AcceptedAt)

	return &i, e
}
