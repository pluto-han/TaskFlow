package mail

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Poll claims at most one message. A 30s lease exceeds the 5s sender deadline.
// Completion is fenced by lease ID, so an old worker cannot acknowledge a new claim.
func (s *Service) Poll(ctx context.Context) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	if _, err := s.pool.Exec(ctx, `WITH expired AS (SELECT token_hash FROM email_tokens WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT 100) DELETE FROM email_tokens WHERE token_hash IN (SELECT token_hash FROM expired)`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `WITH expired AS (SELECT id FROM email_reset_requests WHERE created_at<=clock_timestamp()-interval '1 day' ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED) DELETE FROM email_reset_requests WHERE id IN (SELECT id FROM expired)`); err != nil {
		return err
	}
	// Bound each scrub batch, including the crash-on-fifth-attempt case.
	if _, err := s.pool.Exec(ctx, `WITH doomed AS (SELECT id FROM email_outbox WHERE status IN ('pending','leased') AND (expires_at<=clock_timestamp() OR (attempts>=5 AND (lease_until IS NULL OR lease_until<=clock_timestamp()))) LIMIT 100) UPDATE email_outbox SET recipient=NULL,link=NULL,status='failed',lease_id=NULL,lease_until=NULL WHERE id IN (SELECT id FROM doomed)`); err != nil {
		return err
	}
	var m Message
	var lease string
	var attempts int
	err := s.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM email_outbox WHERE expires_at>clock_timestamp() AND attempts<5 AND link IS NOT NULL
 AND ((status='pending' AND next_attempt_at<=clock_timestamp()) OR (status='leased' AND lease_until<=clock_timestamp()))
 ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE email_outbox o SET status='leased',lease_id=gen_random_uuid(),lease_until=clock_timestamp()+interval '30 seconds',attempts=attempts+1
 FROM candidate c WHERE o.id=c.id RETURNING o.id::text,o.recipient,o.link,o.lease_id::text,o.attempts`).Scan(&m.ID, &m.Recipient, &m.Link, &lease, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	sendErr := s.sender.Send(sendCtx, m)
	cancel()
	outcome := "success"
	if sendErr != nil {
		outcome = "retry"
		if attempts >= 5 {
			outcome = "failed"
		}
	}
	s.observe(outcome)
	// No network error text/body enters persistence or logs.
	_, err = s.pool.Exec(ctx, `UPDATE email_outbox SET
 status=CASE WHEN $3 THEN 'sent' WHEN attempts>=5 THEN 'failed' ELSE 'pending' END,
 recipient=CASE WHEN $3 OR attempts>=5 THEN NULL ELSE recipient END,
 link=CASE WHEN $3 OR attempts>=5 THEN NULL ELSE link END,
 next_attempt_at=clock_timestamp()+make_interval(secs=>$4),lease_id=NULL,lease_until=NULL
 WHERE id=$1 AND lease_id=$2 AND status='leased'`, m.ID, lease, sendErr == nil, float64(30*(int64(1)<<(attempts-1))))
	return err
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll, cancel := context.WithTimeout(ctx, 8*time.Second)
			err := s.Poll(poll)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.observe("error")
			}
		}
	}
}
