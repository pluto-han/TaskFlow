package infratask

import (
	"context"
	"errors"

	pgx "github.com/jackc/pgx/v5"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
)

// All task writers lock source/target projects in the same order as ACL writers,
// then re-read the task. No permission obtained before the locks is trusted.
func (r *Repository) write(
	ctx context.Context,
	actor domainaccount.UserID,
	snapshot *domaintask.Task,
	target *string,
	action func(*db.Queries, *db.Task) error,
) error {
	uid, e := ownerUUID(actor)
	if e != nil {
		return e
	}

	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}

	defer tx.Rollback(context.Background())
	ids := []string{}
	if snapshot != nil && snapshot.ProjectID != nil {
		ids = append(ids, *snapshot.ProjectID)
	}
	if target != nil {
		ids = append(ids, *target)
	}
	if e = infraproject.LockProjects(ctx, tx, ids...); e != nil {
		return e
	}
	if snapshot != nil && snapshot.ProjectID != nil {
		if e = infraproject.Access(ctx, tx, actor, *snapshot.ProjectID, false); e != nil {
			if errors.Is(e, domainproject.ErrNotFound) {
				return domaintask.ErrTaskNotFound
			}
			return e
		}
	}
	if target != nil {
		if e = infraproject.Access(ctx, tx, actor, *target, false); e != nil {
			return e
		}
	}
	q := r.queries.WithTx(tx)
	var current *db.Task
	if snapshot != nil {
		row, e := q.LockTask(ctx, db.LockTaskParams{ID: snapshot.ID, OwnerID: uid})
		if errors.Is(e, pgx.ErrNoRows) {
			return domaintask.ErrTaskNotFound
		}
		if e != nil {
			return e
		}
		if !domaintask.SameProject(projectString(row.ProjectID), snapshot.ProjectID) {
			return domaintask.ErrTaskConflict
		}
		if !domaintask.SameProject(projectString(row.ProjectID), target) && row.OwnerID != uid {
			return domainproject.ErrForbidden
		}

		current = &row
	}
	if e = action(q, current); e != nil {
		return mapProjectConstraint(e)
	}

	return mapProjectConstraint(tx.Commit(ctx))
}
