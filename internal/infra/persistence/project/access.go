package infraproject

import (
	"context"
	"errors"
	"sort"

	pgx "github.com/jackc/pgx/v5"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

// LockProjects is the common lock order for ACL changes and project/task writes.
// READ COMMITTED queries executed AFTER these locks see committed membership.
func LockProjects(ctx context.Context, tx pgx.Tx, ids ...string) error {
	for i, id := range ids {
		if id != "" {
			canonical, err := domainproject.ParseID(id)
			if err != nil {
				return err
			}

			ids[i] = canonical
		}
	}
	sort.Strings(ids)
	previous := ""
	for _, id := range ids {
		if id == "" || id == previous {
			continue
		}
		previous = id
		var value string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM projects WHERE id=$1 FOR UPDATE", id).Scan(&value); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domainproject.ErrNotFound
			}
			return err
		}
	}

	return nil
}

func Access(
	ctx context.Context,
	tx pgx.Tx,
	actor domainaccount.UserID,
	id string,
	ownerOnly bool,
) error {
	var owner, member bool
	err := tx.QueryRow(ctx, `SELECT owner_id=$2, EXISTS(SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=$2) FROM projects p WHERE id=$1`, id, string(actor)).Scan(&owner, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainproject.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !owner && !member {
		return domainproject.ErrNotFound
	}
	if ownerOnly && !owner {
		return domainproject.ErrForbidden
	}

	return nil
}
