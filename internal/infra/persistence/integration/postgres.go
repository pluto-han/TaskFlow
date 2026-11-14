package infraintegration

import (
	"context"
	"errors"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
)

type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool) *Repository { return NewWithClock(pool, time.Now) }

func NewWithClock(pool *pgxpool.Pool, now func() time.Time) *Repository {
	return &Repository{pool: pool, now: now}
}

func ownerID(raw domainaccount.UserID) (pgtype.UUID, error) {
	var id pgtype.UUID
	if e := id.Scan(string(raw)); e != nil || !id.Valid {
		return id, domainaccount.ErrUnauthorized
	}

	return id, nil
}

func (s *Repository) Status(
	ctx context.Context,
	owner domainaccount.UserID,
) (domainintegration.Binding, error) {
	id, e := ownerID(owner)
	if e != nil {
		return domainintegration.Binding{}, e
	}

	row, e := db.New(s.pool).GetDiscordBinding(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return domainintegration.Binding{Enabled: false, Version: 1}, nil
	}

	return binding(row, false), e
}

func (s *Repository) Issue(
	ctx context.Context,
	owner domainaccount.UserID,
	hash []byte,
) (time.Time, error) {
	id, e := ownerID(owner)
	if e != nil {
		return time.Time{}, e
	}

	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return time.Time{}, e
	}

	defer rollback(tx)
	q := db.New(tx)
	if e = q.EnsureDiscordBinding(ctx, id); e != nil {
		return time.Time{}, e
	}

	row, e := q.LockDiscordBinding(ctx, id)
	if e != nil {
		return time.Time{}, e
	}

	now := s.now()
	start := row.RateStartedAt.Time
	count := row.RateCount
	if now.Sub(start) >= time.Minute {
		start = now
		count = 0
	}
	if count >= 3 {
		return time.Time{}, domainintegration.ErrLimited
	}

	expires := now.Add(10 * time.Minute)
	e = q.IssueDiscordCode(ctx, db.IssueDiscordCodeParams{
		OwnerID:       id,
		CodeHash:      hash,
		CodeExpiresAt: stamp(expires),
		RateStartedAt: stamp(start),
		RateCount:     count + 1,
	})
	if e != nil {
		return time.Time{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return time.Time{}, e
	}

	return expires, nil
}

func (s *Repository) Bind(
	ctx context.Context,
	hash []byte,
	target domainintegration.Target,
) (bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return false, e
	}

	defer rollback(tx)
	q := db.New(tx)
	// Replays are acknowledged without reactivating a binding or revealing its owner.
	if _, e = q.FindDiscordInteraction(ctx, target.InteractionID); e == nil {
		return true, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return false, e
	}
	row, e := q.LockDiscordCode(ctx, hash)
	if errors.Is(e, pgx.ErrNoRows) {
		if _, e = q.FindDiscordInteraction(ctx, target.InteractionID); e == nil {
			return true, nil
		}
		return false, domainintegration.ErrCode
	}
	if e != nil {
		return false, e
	}
	if !row.CodeExpiresAt.Valid || !s.now().Before(row.CodeExpiresAt.Time) {
		return false, domainintegration.ErrCode
	}

	inserted, e := q.SaveDiscordInteraction(ctx, db.SaveDiscordInteractionParams{
		InteractionID: target.InteractionID,
		OwnerID:       row.OwnerID,
	})
	if e != nil {
		return false, e
	}
	if inserted == 0 {
		return true, nil
	}

	e = q.ConsumeDiscordCode(ctx, db.ConsumeDiscordCodeParams{
		OwnerID:       row.OwnerID,
		GuildID:       text(target.GuildID),
		ChannelID:     text(target.ChannelID),
		ChannelName:   text(target.ChannelName),
		DiscordUserID: text(target.DiscordUserID),
		BoundAt:       stamp(s.now()),
	})
	if e != nil {
		return false, e
	}
	// Successful channel reauthorization releases a permanent channel failure, but never a timed 429.
	if _, e = tx.Exec(ctx, `UPDATE notification_cooldowns SET until_at=now() WHERE scope='channel:'||$1 AND until_at >= timestamptz '9998-01-01'`, target.ChannelID); e != nil {
		return false, e
	}
	if e = cancelPending(ctx, tx, row.OwnerID); e != nil {
		return false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return false, e
	}

	return false, nil
}

func (s *Repository) Unbind(ctx context.Context, owner domainaccount.UserID, version int64) error {
	id, e := ownerID(owner)
	if e != nil {
		return e
	}
	if version < 1 {
		return domainintegration.ErrInvalid
	}

	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}

	defer rollback(tx)
	q := db.New(tx)
	if e = q.EnsureDiscordBinding(ctx, id); e != nil {
		return e
	}

	row, e := q.LockDiscordBinding(ctx, id)
	if e != nil {
		return e
	}
	if row.Version != version {
		return domainintegration.ErrConflict
	}
	if e = q.UnbindDiscord(ctx, id); e != nil {
		return e
	}
	if e = cancelPending(ctx, tx, id); e != nil {
		return e
	}

	return tx.Commit(ctx)
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func cancelPending(ctx context.Context, tx pgx.Tx, owner pgtype.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE notification_outbox SET status='canceled',lease_token=NULL,lease_until=NULL,updated_at=now()
 WHERE owner_id=$1 AND status IN ('pending','retrying','leased')`, owner)

	return err
}
