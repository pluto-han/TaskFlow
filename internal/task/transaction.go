package task

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/session"
)

func sameProject(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// All task writers lock source/target projects in the same order as ACL writers,
// then re-read the task. No permission obtained before the locks is trusted.
func (r *PostgresRepository) write(ctx context.Context, actor session.UserID, snapshot *Task, target *string, action func(*db.Queries, *db.Task) error) error {
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
	if e = project.LockProjects(ctx, tx, ids...); e != nil {
		return e
	}
	if snapshot != nil && snapshot.ProjectID != nil {
		if e = project.Access(ctx, tx, actor, *snapshot.ProjectID, false); e != nil {
			if errors.Is(e, project.ErrNotFound) {
				return ErrTaskNotFound
			}
			return e
		}
	}
	if target != nil {
		if e = project.Access(ctx, tx, actor, *target, false); e != nil {
			return e
		}
	}
	q := r.queries.WithTx(tx)
	var current *db.Task
	if snapshot != nil {
		row, e := q.LockTask(ctx, db.LockTaskParams{ID: snapshot.ID, OwnerID: uid})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrTaskNotFound
		}
		if e != nil {
			return e
		}
		if !sameProject(projectString(row.ProjectID), snapshot.ProjectID) {
			return ErrTaskConflict
		}
		if !sameProject(projectString(row.ProjectID), target) && row.OwnerID != uid {
			return project.ErrForbidden
		}
		current = &row
	}
	if e = action(q, current); e != nil {
		return mapProjectConstraint(e)
	}
	return mapProjectConstraint(tx.Commit(ctx))
}
