package recurrence

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/session"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewPostgresRepository(p *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: p, q: db.New(p)}
}
func ownerID(owner session.UserID) (pgtype.UUID, error) {
	id, e := parseID(string(owner))
	if e != nil {
		return id, session.ErrUnauthorized
	}
	return id, nil
}
func date(raw string) pgtype.Date {
	d, e := planning.ParseDate(raw)
	return pgtype.Date{Time: d, Valid: e == nil}
}
func domain(row db.RecurrenceRule, e error) (*Rule, error) {
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	r := &Rule{ReminderEnabled: row.ReminderEnabled, ID: row.ID.String(), OwnerID: session.UserID(row.OwnerID.String()), Title: row.Title, Description: row.Description, Priority: int(row.Priority), AnchorDate: row.AnchorDate.Time.Format(time.DateOnly), IntervalDays: int(row.IntervalDays), NextRunDate: row.NextRunDate.Time.Format(time.DateOnly), Paused: row.Paused, Version: row.Version, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.DeletedAt.Valid {
		r.DeletedAt = &row.DeletedAt.Time
	}
	return r, nil
}
func (r *PostgresRepository) Create(ctx context.Context, item *Rule) (*Rule, error) {
	uid, e := ownerID(item.OwnerID)
	if e != nil {
		return nil, e
	}
	id, e := parseID(item.ID)
	if e != nil {
		return nil, e
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	if item.ReminderEnabled {
		if e = requireBinding(ctx, tx, uid); e != nil {
			return nil, e
		}
	}
	saved, e := domain(r.q.WithTx(tx).CreateRecurrence(ctx, db.CreateRecurrenceParams{ID: id, OwnerID: uid, Title: item.Title, Description: item.Description, Priority: int32(item.Priority), AnchorDate: date(item.AnchorDate), IntervalDays: int32(item.IntervalDays), NextRunDate: date(item.NextRunDate), ReminderEnabled: item.ReminderEnabled}))
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return saved, nil
}
func (r *PostgresRepository) Get(ctx context.Context, owner session.UserID, raw string) (*Rule, error) {
	uid, e := ownerID(owner)
	if e != nil {
		return nil, e
	}
	id, e := parseID(raw)
	if e != nil {
		return nil, e
	}
	return domain(r.q.GetRecurrence(ctx, db.GetRecurrenceParams{ID: id, OwnerID: uid}))
}
func (r *PostgresRepository) List(ctx context.Context, owner session.UserID, after string, limit int) ([]Rule, error) {
	uid, e := ownerID(owner)
	if e != nil {
		return nil, e
	}
	if limit < 1 || limit > 101 {
		return nil, ErrInvalid
	}
	var id pgtype.UUID
	if after != "" {
		id, e = parseID(after)
		if e != nil {
			return nil, e
		}
	}
	rows, e := r.q.ListRecurrences(ctx, db.ListRecurrencesParams{OwnerID: uid, AfterID: id, PageLimit: int32(limit)})
	if e != nil {
		return nil, e
	}
	out := make([]Rule, 0, len(rows))
	for _, row := range rows {
		v, _ := domain(row, nil)
		out = append(out, *v)
	}
	return out, nil
}
func (r *PostgresRepository) Change(ctx context.Context, owner session.UserID, raw string, version int64, change func(*Rule, string) error) (*Rule, error) {
	uid, e := ownerID(owner)
	if e != nil {
		return nil, e
	}
	id, e := parseID(raw)
	if e != nil {
		return nil, e
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	q := r.q.WithTx(tx)
	current, e := domain(q.LockRecurrence(ctx, db.LockRecurrenceParams{ID: id, OwnerID: uid}))
	if e != nil {
		return nil, e
	}
	if current.Version != version {
		return nil, ErrConflict
	}
	last, e := q.LatestRecurrenceOccurrence(ctx, db.LatestRecurrenceOccurrenceParams{RuleID: id, OwnerID: uid})
	if e != nil {
		return nil, e
	}
	lastDate := ""
	if last.Valid {
		lastDate = last.Time.Format(time.DateOnly)
	}
	wasReminder := current.ReminderEnabled
	if e = change(current, lastDate); e != nil {
		return nil, e
	}
	if current.ReminderEnabled && !wasReminder {
		if e = requireBinding(ctx, tx, uid); e != nil {
			return nil, e
		}
	}
	if current.Paused || !current.ReminderEnabled || current.DeletedAt != nil {
		_, e = tx.Exec(ctx, `UPDATE notification_outbox SET status='canceled',lease_token=NULL,lease_until=NULL,updated_at=now()
   WHERE rule_id=$1 AND owner_id=$2 AND status IN ('pending','retrying','leased')`, id, uid)
		if e != nil {
			return nil, e
		}
	}
	var deleted pgtype.Timestamptz
	if current.DeletedAt != nil {
		deleted = pgtype.Timestamptz{Time: *current.DeletedAt, Valid: true}
	}
	saved, e := domain(q.UpdateRecurrence(ctx, db.UpdateRecurrenceParams{ID: id, OwnerID: uid, Version: version, Title: current.Title, Description: current.Description, Priority: int32(current.Priority), AnchorDate: date(current.AnchorDate), IntervalDays: int32(current.IntervalDays), NextRunDate: date(current.NextRunDate), ReminderEnabled: current.ReminderEnabled, Paused: current.Paused, DeletedAt: deleted}))
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return saved, nil
}

func requireBinding(ctx context.Context, tx pgx.Tx, owner pgtype.UUID) error {
	var channel pgtype.Text
	err := tx.QueryRow(ctx, "SELECT channel_id FROM discord_bindings WHERE owner_id=$1 FOR SHARE", owner).Scan(&channel)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !channel.Valid {
		return ErrInvalid
	}
	return err
}
