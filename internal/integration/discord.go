// Package integration owns website-account to external-channel binding, not task delivery.
package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/session"
)

var (
	ErrInvalid  = errors.New("invalid binding data")
	ErrCode     = errors.New("invalid or expired binding code")
	ErrLimited  = errors.New("binding code issuance limited")
	ErrConflict = errors.New("binding version changed")
)

type Binding struct {
	Enabled     bool       `json:"enabled"`
	Version     int64      `json:"version"`
	Bound       bool       `json:"bound"`
	GuildID     *string    `json:"guild_id"`
	ChannelID   *string    `json:"channel_id"`
	ChannelName *string    `json:"channel_name"`
	BoundAt     *time.Time `json:"bound_at"`
}
type Code struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}
type UnbindRequest struct {
	Version int64 `json:"version"`
}
type Target struct{ InteractionID, GuildID, ChannelID, ChannelName, DiscordUserID string }
type Service struct {
	pool    *pgxpool.Pool
	enabled bool
	now     func() time.Time
}

func NewService(pool *pgxpool.Pool, enabled bool) *Service {
	return &Service{pool: pool, enabled: enabled, now: time.Now}
}
func (s *Service) Enabled() bool { return s != nil && s.enabled }
func ownerID(raw session.UserID) (pgtype.UUID, error) {
	var id pgtype.UUID
	if e := id.Scan(string(raw)); e != nil || !id.Valid {
		return id, session.ErrUnauthorized
	}
	return id, nil
}
func binding(row db.DiscordBinding, enabled bool) Binding {
	b := Binding{Enabled: enabled, Version: row.Version, Bound: row.ChannelID.Valid}
	if row.ChannelID.Valid {
		b.GuildID = &row.GuildID.String
		b.ChannelID = &row.ChannelID.String
		b.ChannelName = &row.ChannelName.String
		b.BoundAt = &row.BoundAt.Time
	}
	return b
}
func (s *Service) Status(ctx context.Context, owner session.UserID) (Binding, error) {
	id, e := ownerID(owner)
	if e != nil {
		return Binding{}, e
	}
	row, e := db.New(s.pool).GetDiscordBinding(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return Binding{Enabled: s.Enabled(), Version: 1}, nil
	}
	return binding(row, s.Enabled()), e
}
func (s *Service) Issue(ctx context.Context, owner session.UserID) (Code, error) {
	if !s.Enabled() {
		return Code{}, ErrInvalid
	}
	id, e := ownerID(owner)
	if e != nil {
		return Code{}, e
	}
	var secret [32]byte
	if _, e = rand.Read(secret[:]); e != nil {
		return Code{}, e
	}
	raw := base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(raw))
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Code{}, e
	}
	defer rollback(tx)
	q := db.New(tx)
	if e = q.EnsureDiscordBinding(ctx, id); e != nil {
		return Code{}, e
	}
	row, e := q.LockDiscordBinding(ctx, id)
	if e != nil {
		return Code{}, e
	}
	now := s.now()
	start := row.RateStartedAt.Time
	count := row.RateCount
	if now.Sub(start) >= time.Minute {
		start = now
		count = 0
	}
	if count >= 3 {
		return Code{}, ErrLimited
	}
	expires := now.Add(10 * time.Minute)
	e = q.IssueDiscordCode(ctx, db.IssueDiscordCodeParams{OwnerID: id, CodeHash: hash[:], CodeExpiresAt: stamp(expires), RateStartedAt: stamp(start), RateCount: count + 1})
	if e != nil {
		return Code{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Code{}, e
	}
	return Code{Code: raw, ExpiresAt: expires}, nil
}
func ValidCode(code string) bool {
	if len(code) != 43 {
		return false
	}
	decoded, e := base64.RawURLEncoding.Strict().DecodeString(code)
	return e == nil && len(decoded) == 32
}
func (s *Service) Bind(ctx context.Context, code string, target Target) (bool, error) {
	if !s.Enabled() || !ValidCode(code) {
		return false, ErrCode
	}
	hash := sha256.Sum256([]byte(code))
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
	row, e := q.LockDiscordCode(ctx, hash[:])
	if errors.Is(e, pgx.ErrNoRows) {
		if _, e = q.FindDiscordInteraction(ctx, target.InteractionID); e == nil {
			return true, nil
		}
		return false, ErrCode
	}
	if e != nil {
		return false, e
	}
	if !row.CodeExpiresAt.Valid || !s.now().Before(row.CodeExpiresAt.Time) {
		return false, ErrCode
	}
	inserted, e := q.SaveDiscordInteraction(ctx, db.SaveDiscordInteractionParams{InteractionID: target.InteractionID, OwnerID: row.OwnerID})
	if e != nil {
		return false, e
	}
	if inserted == 0 {
		return true, nil
	}
	e = q.ConsumeDiscordCode(ctx, db.ConsumeDiscordCodeParams{OwnerID: row.OwnerID, GuildID: text(target.GuildID), ChannelID: text(target.ChannelID), ChannelName: text(target.ChannelName), DiscordUserID: text(target.DiscordUserID), BoundAt: stamp(s.now())})
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
func (s *Service) Unbind(ctx context.Context, owner session.UserID, version int64) error {
	id, e := ownerID(owner)
	if e != nil {
		return e
	}
	if version < 1 {
		return ErrInvalid
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
		return ErrConflict
	}
	if e = q.UnbindDiscord(ctx, id); e != nil {
		return e
	}
	if e = cancelPending(ctx, tx, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
func text(s string) pgtype.Text            { return pgtype.Text{String: s, Valid: true} }
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
