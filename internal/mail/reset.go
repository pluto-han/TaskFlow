package mail

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

var (
	ErrResetToken    = errors.New("invalid password reset capability")
	ErrResetPassword = errors.New("invalid reset password")
	ErrResetEmail    = errors.New("invalid reset email")
)

type ResetRequest struct {
	Email string `json:"email"`
}
type ResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// WithPasswords supplies the same bounded hashing primitive used by registration.
// Set dependencies during construction, never while serving requests.
func (s *Service) WithPasswords(p applicationaccount.PasswordHasher) *Service {
	s.passwords = p
	return s
}

// RequestReset returns the same successful result for missing, throttled and
// accepted targets. Source/global HTTP throttles remain visible and account-independent.
func (s *Service) RequestReset(ctx context.Context, input ResetRequest) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	address, valid := domainaccount.NormalizeEmail(input.Email)
	if !valid {
		return ErrResetEmail
	}
	target := sha256.Sum256([]byte(address))
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
 OR (SELECT count(*) FROM email_outbox WHERE status IN ('pending','leased'))>=1000`, target[:]).Scan(&limited)
	if err != nil {
		return err
	}
	if limited {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO email_reset_requests(target_hash) VALUES($1)", target[:]); err != nil {
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
 SELECT gen_random_uuid(),token_hash,$1,$3,expires_at FROM issued`, address, hash[:], s.origin+"/reset-password#token="+token)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConfirmReset hashes outside the transaction, then checks the issuance epoch
// again under the same user-row lock as login, logout and password changes.
func (s *Service) ConfirmReset(ctx context.Context, input ResetConfirmRequest) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	hash, err := tokenHash(input.Token)
	if err != nil {
		return ErrResetToken
	}
	if !domainaccount.ValidPassword(input.NewPassword) {
		return ErrResetPassword
	}
	var owner string
	var version int64
	err = s.pool.QueryRow(ctx, `SELECT t.user_id::text,t.credentials_version FROM email_tokens t JOIN users u ON u.id=t.user_id
 WHERE t.token_hash=$1 AND t.purpose='reset_password' AND t.consumed_at IS NULL AND t.expires_at>clock_timestamp()
 AND t.credentials_version=u.credentials_version`, hash).Scan(&owner, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrResetToken
	}
	if err != nil {
		return err
	}
	encoded, err := s.passwords.HashPassword(ctx, input.NewPassword)
	if err != nil {
		return err
	}
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
		return ErrResetToken
	}
	tag, err := tx.Exec(ctx, `UPDATE email_tokens SET consumed_at=clock_timestamp() WHERE token_hash=$1 AND user_id=$2 AND purpose='reset_password' AND credentials_version=$3 AND consumed_at IS NULL AND expires_at>clock_timestamp()`, hash, owner, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrResetToken
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
