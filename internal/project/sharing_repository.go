package project

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/pluto-han/taskflow/internal/session"
)

func (r *PostgresRepository) withAccess(ctx context.Context, a session.UserID, id string, owner bool, action func(pgx.Tx) error) error {
	if _, e := ownerUUID(a); e != nil {
		return e
	}
	if _, e := idUUID(id); e != nil {
		return e
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if e = LockProjects(ctx, tx, id); e != nil {
		return e
	}
	if e = Access(ctx, tx, a, id, owner); e != nil {
		return e
	}
	if e = action(tx); e != nil {
		return databaseError(e)
	}
	return databaseError(tx.Commit(ctx))
}
func (r *PostgresRepository) Members(ctx context.Context, a session.UserID, id string) (result []Member, err error) {
	result = []Member{}
	err = r.withAccess(ctx, a, id, false, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT owner_id::text,'owner' FROM projects WHERE id=$1 UNION ALL SELECT user_id::text,role FROM project_memberships WHERE project_id=$1 ORDER BY 2 DESC,1`, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if e = rows.Scan(&m.UserID, &m.Role); e != nil {
				return e
			}
			result = append(result, m)
		}
		return rows.Err()
	})
	return
}

const invitationColumns = "id::text,project_id::text,invitee_user_id::text,created_at,expires_at,revoked_at,accepted_at"

func scanInvitation(row pgx.Row) (*Invitation, error) {
	var i Invitation
	e := row.Scan(&i.ID, &i.ProjectID, &i.InviteeUserID, &i.CreatedAt, &i.ExpiresAt, &i.RevokedAt, &i.AcceptedAt)
	return &i, e
}
func (r *PostgresRepository) Invite(ctx context.Context, a session.UserID, id, recipient, iid string, hash [32]byte) (result *Invitation, err error) {
	err = r.withAccess(ctx, a, id, true, func(tx pgx.Tx) error {
		var valid bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$2) AND NOT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND owner_id=$2) AND NOT EXISTS(SELECT 1 FROM project_memberships WHERE project_id=$1 AND user_id=$2)`, id, recipient).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return ErrInvalidInvitee
		}
		// Reissuing an invitation invalidates previous outstanding tokens for this recipient.
		if _, e := tx.Exec(ctx, `UPDATE project_invitations SET revoked_at=clock_timestamp() WHERE project_id=$1 AND invitee_user_id=$2 AND revoked_at IS NULL AND accepted_at IS NULL`, id, recipient); e != nil {
			return e
		}
		var e error
		result, e = scanInvitation(tx.QueryRow(ctx, `INSERT INTO project_invitations(id,project_id,invitee_user_id,created_by,token_hash,expires_at) VALUES($1,$2,$3,$4,$5,clock_timestamp()+interval '24 hours') RETURNING `+invitationColumns, iid, id, recipient, string(a), hash[:]))
		return e
	})
	return
}
func (r *PostgresRepository) Invitations(ctx context.Context, a session.UserID, id string) (result []Invitation, err error) {
	result = []Invitation{}
	err = r.withAccess(ctx, a, id, true, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT `+invitationColumns+` FROM project_invitations WHERE project_id=$1 ORDER BY created_at DESC,id`, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			i, e := scanInvitation(rows)
			if e != nil {
				return e
			}
			result = append(result, *i)
		}
		return rows.Err()
	})
	return
}
func (r *PostgresRepository) RevokeInvitation(ctx context.Context, a session.UserID, id, iid string) error {
	return r.withAccess(ctx, a, id, true, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE project_invitations SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE project_id=$1 AND id=$2 AND accepted_at IS NULL`, id, iid)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return ErrInvitationInvalid
		}
		return nil
	})
}
func (r *PostgresRepository) AcceptInvitation(ctx context.Context, a session.UserID, hash [32]byte) (string, error) {
	if _, e := ownerUUID(a); e != nil {
		return "", e
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(context.Background())
	var pid string
	e = tx.QueryRow(ctx, `SELECT project_id::text FROM project_invitations WHERE token_hash=$1 AND invitee_user_id=$2`, hash[:], string(a)).Scan(&pid)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrInvitationInvalid
	}
	if e != nil {
		return "", e
	}
	if e = LockProjects(ctx, tx, pid); e != nil {
		if errors.Is(e, ErrNotFound) {
			return "", ErrInvitationInvalid
		}
		return "", e
	}
	var iid string
	e = tx.QueryRow(ctx, `SELECT id::text FROM project_invitations WHERE token_hash=$1 AND project_id=$2 AND invitee_user_id=$3 AND expires_at>clock_timestamp() AND revoked_at IS NULL AND accepted_at IS NULL FOR UPDATE`, hash[:], pid, string(a)).Scan(&iid)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrInvitationInvalid
	}
	if e != nil {
		return "", e
	}
	var owner bool
	if e = tx.QueryRow(ctx, "SELECT owner_id=$2 FROM projects WHERE id=$1", pid, string(a)).Scan(&owner); e != nil {
		return "", e
	}
	if owner {
		return "", ErrInvitationInvalid
	}
	tag, e := tx.Exec(ctx, `INSERT INTO project_memberships(project_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, pid, string(a))
	if e != nil {
		return "", e
	}
	if tag.RowsAffected() != 1 {
		return "", ErrInvitationInvalid
	}
	if _, e = tx.Exec(ctx, `UPDATE project_invitations SET accepted_at=clock_timestamp() WHERE id=$1`, iid); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return pid, nil
}
func (r *PostgresRepository) RemoveMember(ctx context.Context, a session.UserID, id, member string, leave bool) error {
	return r.withAccess(ctx, a, id, !leave, func(tx pgx.Tx) error {
		var owner bool
		if e := tx.QueryRow(ctx, "SELECT owner_id=$2 FROM projects WHERE id=$1", id, member).Scan(&owner); e != nil {
			return e
		}
		if owner {
			return ErrOwnerRequired
		}
		tag, e := tx.Exec(ctx, "DELETE FROM project_memberships WHERE project_id=$1 AND user_id=$2", id, member)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, e = tx.Exec(ctx, `UPDATE project_invitations SET revoked_at=clock_timestamp() WHERE project_id=$1 AND invitee_user_id=$2 AND revoked_at IS NULL AND accepted_at IS NULL`, id, member)
		return e
	})
}
func (r *PostgresRepository) Transfer(ctx context.Context, a session.UserID, id, target string) error {
	return r.withAccess(ctx, a, id, true, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, "DELETE FROM project_memberships WHERE project_id=$1 AND user_id=$2", id, target)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrInvalidInvitee
		}
		if _, e = tx.Exec(ctx, "UPDATE projects SET owner_id=$2,updated_at=clock_timestamp() WHERE id=$1 AND owner_id=$3", id, target, string(a)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO project_memberships(project_id,user_id) VALUES($1,$2)", id, string(a)); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE project_invitations SET revoked_at=clock_timestamp() WHERE project_id=$1 AND revoked_at IS NULL AND accepted_at IS NULL", id)
		return e
	})
}
