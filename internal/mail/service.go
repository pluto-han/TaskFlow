// Package mail owns verification capabilities and a separate transactional email outbox.
package mail

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	"github.com/pluto-han/taskflow/internal/session"
)

var (
	ErrDisabled = errors.New("email disabled")
	ErrLimited  = errors.New("email request limited")
	ErrToken    = errors.New("invalid verification token")
)

// Sender must honor cancellation and must never log messages or credentials.
// Delivery is at least once: a crash after sending can cause duplicate mail.
type Sender interface {
	Send(context.Context, Message) error
}
type Message struct{ ID, Recipient, Link string }
type Observer interface{ MailResult(string) }
type Service struct {
	pool      *pgxpool.Pool
	sender    Sender
	origin    string
	observer  Observer
	passwords applicationaccount.PasswordHasher
}

// A nil sender disables both endpoints without querying the database.
func New(pool *pgxpool.Pool, sender Sender, origin string) (*Service, error) {
	if sender != nil {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
			return nil, errors.New("mail requires a fixed HTTPS or loopback origin")
		}
	}
	return &Service{pool: pool, sender: sender, origin: origin, passwords: infrapassword.PasswordHasher{}}, nil
}
func (s *Service) Enabled() bool                    { return s != nil && s.sender != nil }
func (s *Service) WithObserver(o Observer) *Service { s.observer = o; return s }
func (s *Service) observe(outcome string) {
	if s.observer != nil {
		s.observer.MailResult(outcome)
	}
}

// Request takes identity only from authentication, never from a submitted email/user ID.
func (s *Service) Request(ctx context.Context, p session.Principal) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// A short global transaction lock serializes queue capacity checks across replicas.
	// It is never held during delivery. User-row-before-session matches credential operations.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(480048)"); err != nil {
		return err
	}
	var email string
	var verified bool
	err = tx.QueryRow(ctx, "SELECT email,email_verified_at IS NOT NULL FROM users WHERE id=$1 FOR UPDATE", string(p.UserID)).Scan(&email, &verified)
	if err != nil {
		return err
	}
	var active bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp())", p.SessionID, string(p.UserID)).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return session.ErrUnauthorized
	}
	if verified {
		return tx.Commit(ctx)
	}
	var limited bool
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM email_tokens WHERE user_id=$1 AND created_at>clock_timestamp()-interval '1 day')>=3
 OR EXISTS(SELECT 1 FROM email_tokens WHERE user_id=$1 AND created_at>clock_timestamp()-interval '1 minute')
 OR (SELECT count(*) FROM email_tokens WHERE created_at>clock_timestamp()-interval '1 minute')>=30
 OR (SELECT count(*) FROM email_outbox WHERE status IN ('pending','leased'))>=1000`, string(p.UserID)).Scan(&limited)
	if err != nil {
		return err
	}
	if limited {
		return ErrLimited
	}
	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO email_tokens(token_hash,user_id,purpose,expires_at) VALUES($1,$2,'verify_email',clock_timestamp()+interval '24 hours') RETURNING expires_at`, hash[:], string(p.UserID)).Scan(&expires)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_outbox(id,token_hash,recipient,link,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,$4)`, hash[:], email, s.origin+"/verify-email#token="+token, expires)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func tokenHash(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != raw {
		return nil, ErrToken
	}
	hash := sha256.Sum256([]byte(raw))
	return hash[:], nil
}

// Confirm is an explicit POST capability operation, independent of login state.
func (s *Service) Confirm(ctx context.Context, raw string) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	hash, err := tokenHash(raw)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var owner string
	// Discover the owner, then lock users before tokens to keep a single lock order.
	err = tx.QueryRow(ctx, "SELECT user_id::text FROM email_tokens WHERE token_hash=$1 AND purpose='verify_email'", hash).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrToken
	}
	if err != nil {
		return err
	}
	var id string
	if err = tx.QueryRow(ctx, "SELECT id::text FROM users WHERE id=$1 FOR UPDATE", owner).Scan(&id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE email_tokens SET consumed_at=clock_timestamp() WHERE token_hash=$1 AND user_id=$2 AND purpose='verify_email' AND consumed_at IS NULL AND expires_at>clock_timestamp()`, hash, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrToken
	}
	if _, err = tx.Exec(ctx, "UPDATE users SET email_verified_at=COALESCE(email_verified_at,clock_timestamp()) WHERE id=$1", owner); err != nil {
		return err
	}
	// Other links become unusable, and sensitive queued payloads are scrubbed immediately.
	if _, err = tx.Exec(ctx, "UPDATE email_tokens SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE user_id=$1 AND purpose='verify_email'", owner); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_outbox SET recipient=NULL,link=NULL,status='failed',lease_id=NULL,lease_until=NULL WHERE token_hash IN (SELECT token_hash FROM email_tokens WHERE user_id=$1 AND purpose='verify_email') AND status IN ('pending','leased')`, owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
