package infraaccount

import (
	"context"
	"errors"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

type CapabilityRepository struct{ pool *pgxpool.Pool }

func NewCapabilities(pool *pgxpool.Pool) *CapabilityRepository {
	return &CapabilityRepository{pool: pool}
}

func (s *CapabilityRepository) Request(
	ctx context.Context,
	p domainsession.Principal,
	hash []byte,
	link string,
) error {
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
		return domainaccount.ErrUnauthorized
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
		return domainaccount.ErrLimited
	}

	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO email_tokens(token_hash,user_id,purpose,expires_at) VALUES($1,$2,'verify_email',clock_timestamp()+interval '24 hours') RETURNING expires_at`, hash, string(p.UserID)).Scan(&expires)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `INSERT INTO email_outbox(id,token_hash,recipient,link,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,$4)`, hash, email, link, expires)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *CapabilityRepository) Confirm(ctx context.Context, hash []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)
	var owner string
	// Discover the owner, then lock users before tokens to keep a single lock order.
	err = tx.QueryRow(ctx, "SELECT user_id::text FROM email_tokens WHERE token_hash=$1 AND purpose='verify_email'", hash).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainaccount.ErrToken
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
		return domainaccount.ErrToken
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

func (s *CapabilityRepository) RequestReset(
	ctx context.Context,
	address string,
	target, hash []byte,
	link string,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(480048)"); err != nil {
		return err
	}

	var limited bool
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM email_reset_requests WHERE target_hash=$1 AND created_at>clock_timestamp()-interval '1 day')>=3
 OR EXISTS(SELECT 1 FROM email_reset_requests WHERE target_hash=$1 AND created_at>clock_timestamp()-interval '1 minute')
 OR (SELECT count(*) FROM email_reset_requests WHERE created_at>clock_timestamp()-interval '1 minute')>=30
 OR (SELECT count(*) FROM email_tokens WHERE created_at>clock_timestamp()-interval '1 minute')>=30
 OR (SELECT count(*) FROM email_outbox WHERE status IN ('pending','leased'))>=1000`, target).Scan(&limited)
	if err != nil {
		return err
	}
	if limited {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO email_reset_requests(target_hash) VALUES($1)", target); err != nil {
		return err
	}

	// Both existing and missing accounts execute the same statement sequence.
	// No token or delivery row is created when the locked account CTE is empty.
	_, err = tx.Exec(ctx, `WITH account AS MATERIALIZED (
 SELECT id,credentials_version FROM users WHERE email=$1 FOR UPDATE
 ), issued AS (
 INSERT INTO email_tokens(token_hash,user_id,purpose,credentials_version,expires_at)
 SELECT $2,id,'reset_password',credentials_version,clock_timestamp()+interval '30 minutes' FROM account
 RETURNING token_hash,expires_at
 ) INSERT INTO email_outbox(id,token_hash,recipient,link,expires_at)
 SELECT gen_random_uuid(),token_hash,$1,$3,expires_at FROM issued`, address, hash, link)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ConfirmReset hashes outside the transaction, then checks the issuance epoch
// again under the same user-row lock as login, logout and password changes.
func (s *CapabilityRepository) FindReset(
	ctx context.Context,
	hash []byte,
) (applicationaccount.ResetIdentity, error) {
	var owner string
	var version int64
	err := s.pool.QueryRow(ctx, `SELECT t.user_id::text,t.credentials_version FROM email_tokens t JOIN users u ON u.id=t.user_id
 WHERE t.token_hash=$1 AND t.purpose='reset_password' AND t.consumed_at IS NULL AND t.expires_at>clock_timestamp()
 AND t.credentials_version=u.credentials_version`, hash).Scan(&owner, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return applicationaccount.ResetIdentity{}, domainaccount.ErrResetToken
	}
	if err != nil {
		return applicationaccount.ResetIdentity{}, err
	}

	return applicationaccount.ResetIdentity{Owner: owner, Version: version}, nil
}

func (s *CapabilityRepository) ConfirmReset(
	ctx context.Context,
	hash []byte,
	identity applicationaccount.ResetIdentity,
	encoded string,
) error {
	owner, version := identity.Owner, identity.Version
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(context.Background())
	var current int64
	if err = tx.QueryRow(ctx, "SELECT credentials_version FROM users WHERE id=$1 FOR UPDATE", owner).Scan(&current); err != nil {
		return err
	}
	if current != version {
		return domainaccount.ErrResetToken
	}

	tag, err := tx.Exec(ctx, `UPDATE email_tokens SET consumed_at=clock_timestamp() WHERE token_hash=$1 AND user_id=$2 AND purpose='reset_password' AND credentials_version=$3 AND consumed_at IS NULL AND expires_at>clock_timestamp()`, hash, owner, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domainaccount.ErrResetToken
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,credentials_version=credentials_version+1,email_verified_at=COALESCE(email_verified_at,clock_timestamp()) WHERE id=$1`, owner, encoded); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE sessions SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE user_id=$1", owner); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE email_tokens SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE user_id=$1 AND purpose='reset_password'", owner); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_outbox SET recipient=NULL,link=NULL,status='failed',lease_id=NULL,lease_until=NULL
 WHERE token_hash IN (SELECT token_hash FROM email_tokens WHERE user_id=$1 AND purpose='reset_password') AND status IN ('pending','leased')`, owner); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *CapabilityRepository) VerifiedAt(
	ctx context.Context,
	owner domainaccount.UserID,
) (*time.Time, error) {
	var at *time.Time
	err := s.pool.QueryRow(ctx, "SELECT email_verified_at FROM users WHERE id=$1", string(owner)).Scan(&at)

	return at, err
}
