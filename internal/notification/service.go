// Package notification implements the transactional outbox sender.
package notification

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/discord"
)

const leaseDuration = 30 * time.Second
const maxAttempts = 8

type Event struct {
	ID            string    `json:"id"`
	RuleID        string    `json:"rule_id"`
	TaskID        string    `json:"task_id"`
	ScheduledDate string    `json:"scheduled_date"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error"`
	CreatedAt     time.Time `json:"created_at"`
}
type claim struct {
	Event
	Token, Channel, MentionUser string
	Reclaimed                   bool
}
type Observer interface {
	ObserveNotificationPoll(bool)
	ObserveNotificationSend(int, bool)
	ObserveNotificationReclaim()
}
type Sender interface {
	Send(context.Context, string, string, string, string) (discord.SendResult, error)
}
type Service struct {
	pool       *pgxpool.Pool
	sender     Sender
	logger     *slog.Logger
	credential string
	observer   Observer
}

// WithObserver must be called before the worker starts.
func (s *Service) WithObserver(observer Observer) *Service { s.observer = observer; return s }

func NewService(pool *pgxpool.Pool, sender Sender) *Service {
	credential := "default"
	if item, ok := sender.(interface{ CredentialFingerprint() string }); ok {
		credential = item.CredentialFingerprint()
	}
	return &Service{pool: pool, sender: sender, logger: slog.Default(), credential: credential}
}
func (s *Service) List(ctx context.Context, owner string) ([]Event, error) {
	rows, e := s.pool.Query(ctx, `SELECT id::text,rule_id::text,task_id,scheduled_date::text,title,CASE WHEN status IN ('pending','retrying','leased') AND expires_at<=now() THEN 'expired' ELSE status END,attempts,last_error,created_at
 FROM notification_outbox WHERE owner_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, owner)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var x Event
		if e = rows.Scan(&x.ID, &x.RuleID, &x.TaskID, &x.ScheduledDate, &x.Title, &x.Status, &x.Attempts, &x.LastError, &x.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func randomUUID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:]), nil
}
func (s *Service) claim(ctx context.Context) (*claim, error) {
	token, e := randomUUID()
	if e != nil {
		return nil, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer rollback(tx)
	// Expire or cancel at most 100 rows per cycle, without waiting on another worker.
	_, e = tx.Exec(ctx, `WITH stale AS (
 SELECT o.id, CASE WHEN o.expires_at<=now() THEN 'expired' WHEN o.attempts>=8 THEN 'failed' ELSE 'canceled' END AS next
 FROM notification_outbox o
 LEFT JOIN recurrence_rules r ON r.id=o.rule_id AND r.owner_id=o.owner_id
 LEFT JOIN discord_bindings b ON b.owner_id=o.owner_id
 WHERE (o.status IN ('pending','retrying') OR (o.status='leased' AND o.lease_until<=now()))
 AND (o.expires_at<=now() OR o.attempts>=8 OR r.id IS NULL OR r.paused OR r.deleted_at IS NOT NULL OR NOT r.reminder_enabled
      OR b.channel_id IS NULL OR b.channel_id<>o.channel_id OR b.version<>o.binding_version)
 ORDER BY o.created_at LIMIT 100 FOR UPDATE OF o SKIP LOCKED)
 UPDATE notification_outbox o SET status=stale.next,lease_token=NULL,lease_until=NULL,updated_at=now()
 FROM stale WHERE o.id=stale.id`)
	if e != nil {
		return nil, e
	}
	var c claim
	e = tx.QueryRow(ctx, `WITH candidate AS (
 SELECT o.id, o.status='leased' AS reclaimed FROM notification_outbox o
 WHERE (o.status IN ('pending','retrying') OR (o.status='leased' AND o.lease_until<=now()))
 AND o.next_attempt_at<=now() AND o.expires_at>now()
 AND NOT EXISTS(SELECT 1 FROM notification_cooldowns cd WHERE cd.scope IN ('global','channel:'||o.channel_id,'credential:'||$2) AND cd.until_at>now())
 ORDER BY o.next_attempt_at,o.id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE notification_outbox o SET status='leased',lease_token=$1,lease_until=now()+interval '30 seconds',
 attempts=attempts+1,updated_at=now()
 FROM candidate WHERE o.id=candidate.id
 RETURNING o.id::text,o.rule_id::text,o.task_id,o.scheduled_date::text,o.title,o.status,o.attempts,o.last_error,o.created_at,o.lease_token::text,o.channel_id,candidate.reclaimed`, token, s.credential).
		Scan(&c.ID, &c.RuleID, &c.TaskID, &c.ScheduledDate, &c.Title, &c.Status, &c.Attempts, &c.LastError, &c.CreatedAt, &c.Token, &c.Channel, &c.Reclaimed)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = tx.Commit(ctx); e != nil {
			return nil, e
		}
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return &c, nil
}
func (s *Service) preflight(ctx context.Context, c *claim) (bool, error) {
	// Fencing checks include active lease and current rule/binding generation.
	var mentioned string
	e := s.pool.QueryRow(ctx, `SELECT COALESCE((
 SELECT b.discord_user_id FROM notification_outbox o JOIN recurrence_rules r ON r.id=o.rule_id AND r.owner_id=o.owner_id
 JOIN discord_bindings b ON b.owner_id=o.owner_id
 WHERE o.id=$1 AND o.status='leased' AND o.lease_token=$2 AND o.lease_until>now() AND o.expires_at>now()
 AND r.deleted_at IS NULL AND NOT r.paused AND r.reminder_enabled
 AND b.version=o.binding_version AND b.channel_id=o.channel_id), '')`, c.ID, c.Token).Scan(&mentioned)
	if e != nil {
		return false, e
	}
	valid := discord.Snowflake(mentioned)

	if valid {
		// Another worker may have received a rate limit after this lease was taken.
		var cooled bool
		e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_cooldowns
    WHERE scope IN ('global','channel:'||$1,'credential:'||$2) AND until_at>now())`, c.Channel, s.credential).Scan(&cooled)
		if e != nil {
			return false, e
		}
		if cooled {
			_, e = s.pool.Exec(ctx, `UPDATE notification_outbox SET status='retrying',attempts=GREATEST(attempts-1,0),
     lease_token=NULL,lease_until=NULL,updated_at=now()
     WHERE id=$1 AND status='leased' AND lease_token=$2 AND lease_until>now()`, c.ID, c.Token)
			return false, e
		}
		c.MentionUser = mentioned
		return true, nil
	}
	_, e = s.pool.Exec(ctx, `UPDATE notification_outbox SET status=CASE WHEN expires_at<=now() THEN 'expired' ELSE 'canceled' END,
 lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1 AND status='leased' AND lease_token=$2`, c.ID, c.Token)
	return false, e
}
func (s *Service) finish(ctx context.Context, c *claim, result discord.SendResult, sendErr error) error {
	status, reason := "sent", ""
	delay := time.Duration(0)
	switch {
	case sendErr != nil:
		status, reason, delay = "retrying", "network", backoff(c.Attempts)
	case result.Status >= 200 && result.Status < 300:
	case result.Status == 429:
		status, reason, delay = "retrying", "rate_limited", result.RetryAfter
	case result.Status == 401:
		status, reason = "failed", "discord_token_invalid"
	case result.Status == 403:
		status, reason = "failed", "discord_permission"
	case result.Status == 404:
		status, reason = "failed", "discord_channel_missing"
	case result.Status >= 500:
		status, reason, delay = "retrying", "discord_unavailable", backoff(c.Attempts)
	default:
		status, reason = "failed", "discord_rejected"
	}
	if status == "retrying" && c.Attempts >= maxAttempts {
		status, reason = "failed", "attempts_exhausted"
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollback(tx)
	// Apply cooldown only for a response belonging to the current unexpired lease.
	tag, e := tx.Exec(ctx, `UPDATE notification_outbox SET
 status=CASE WHEN expires_at<=now() AND $3='retrying' THEN 'expired' ELSE $3 END,
 last_error=$4,next_attempt_at=now()+$5 * interval '1 second',lease_token=NULL,lease_until=NULL,updated_at=now()
 WHERE id=$1 AND status='leased' AND lease_token=$2 AND lease_until>now()`, c.ID, c.Token, status, reason, delay.Seconds())
	if e != nil {
		return e
	}
	if sendErr == nil && tag.RowsAffected() == 1 && (result.Status == 429 || result.Status == 401 || result.Status == 403 || result.Status == 404 || (result.Status >= 200 && result.Status < 300 && result.RetryAfter > 0)) {
		scope := "channel:" + c.Channel
		until := time.Now().Add(delay)
		if result.Status >= 200 && result.Status < 300 {
			until = time.Now().Add(result.RetryAfter)
		}
		if result.Global {
			scope = "global"
		}
		if result.Status == 401 {
			scope = "credential:" + s.credential
			until = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		}
		if result.Status == 403 || result.Status == 404 {
			scope = "channel:" + c.Channel
			until = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		}
		_, e = tx.Exec(ctx, `INSERT INTO notification_cooldowns(scope,until_at) VALUES($1,$2)
 ON CONFLICT(scope) DO UPDATE SET until_at=GREATEST(notification_cooldowns.until_at,excluded.until_at)`, scope, until)
		if e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	base := time.Duration(1<<uint(attempt-1)) * 15 * time.Second
	var b [1]byte
	if _, e := rand.Read(b[:]); e != nil {
		return base
	}
	return base + time.Duration(b[0])*base/1024
}
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	if s.sender == nil {
		return false, errors.New("notification sender not configured")
	}
	c, e := s.claim(ctx)
	if e != nil || c == nil {
		return false, e
	}
	if c.Reclaimed && s.observer != nil {
		s.observer.ObserveNotificationReclaim()
	}
	valid, e := s.preflight(ctx, c)
	if e != nil || !valid {
		return true, e
	}
	nonce := c.ID[:8] + c.ID[9:13] + c.ID[14:18] + c.ID[19:23] + c.ID[24:28] // stable and at most 25 characters
	result, sendErr := s.sender.Send(ctx, c.Channel, nonce, c.Title, c.MentionUser)
	if s.observer != nil {
		s.observer.ObserveNotificationSend(result.Status, sendErr != nil)
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if e = s.finish(ctx, c, result, sendErr); e != nil {
		return true, e
	}
	return true, nil
}
func (s *Service) Run(ctx context.Context) {
	delay := time.Duration(0)
	for ctx.Err() == nil {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		batch, cancel := context.WithTimeout(ctx, 10*time.Second)
		found, e := s.ProcessOne(batch)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if s.observer != nil {
			s.observer.ObserveNotificationPoll(e == nil)
		}
		if e != nil {
			s.logger.Warn("notification_worker_failed")
			delay = 30 * time.Second
		} else if found {
			delay = time.Second
		} else {
			delay = 5 * time.Second
		}
	}
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
