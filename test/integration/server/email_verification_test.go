package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pluto-han/taskflow/internal/httpapi"
	email "github.com/pluto-han/taskflow/internal/mail"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
)

func mailService(t *testing.T, f *accountFixture, sender email.Sender) *email.Service {
	t.Helper()
	// Each mail test owns the mail data inside the guarded lesson47 fixture.
	if _, err := f.admin.Exec(f.ctx, "DELETE FROM email_tokens; DELETE FROM email_reset_requests"); err != nil {
		t.Fatal("mail fixture cleanup failed")
	}
	t.Cleanup(func() {
		if _, err := f.admin.Exec(f.ctx, "DELETE FROM email_tokens; DELETE FROM email_reset_requests"); err != nil {
			t.Error("mail fixture cleanup failed")
		}
	})
	s, err := email.New(f.pool, sender, "https://taskflow.example.com")
	if err != nil {
		t.Fatal("mail setup failed")
	}
	return s
}
func verificationToken(t *testing.T, s *email.Service, inbox *email.MemorySender, f *accountFixture, p session.Principal) string {
	t.Helper()
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal("verification request failed", err)
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal("delivery poll failed", err)
	}
	messages := inbox.Take()
	if len(messages) != 1 {
		t.Fatalf("expected one email, got %d", len(messages))
	}
	var recipient string
	if err := f.admin.QueryRow(f.ctx, "SELECT email FROM users WHERE id=$1", string(p.UserID)).Scan(&recipient); err != nil || recipient != messages[0].Recipient {
		t.Fatal("email sent to a different account")
	}
	if !strings.HasPrefix(messages[0].Link, "https://taskflow.example.com/verify-email#token=") {
		t.Fatal("unsafe mail link")
	}
	return strings.TrimPrefix(messages[0].Link, "https://taskflow.example.com/verify-email#token=")
}
func TestEmailVerificationLifecycle(t *testing.T) {
	f := accountDatabase(t)
	u, login, p := f.account(t)
	other, _, _ := f.account(t)
	inbox := &email.MemorySender{}
	s := mailService(t, f, inbox)
	var verified bool
	if err := f.admin.QueryRow(f.ctx, "SELECT email_verified_at IS NOT NULL FROM users WHERE id=$1", u.ID).Scan(&verified); err != nil || verified {
		t.Fatal("new account falsely verified")
	}
	token := verificationToken(t, s, inbox, f, p)
	var material bool
	if err := f.admin.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM email_outbox WHERE recipient IS NOT NULL OR link IS NOT NULL)").Scan(&material); err != nil || material {
		t.Fatal("sent materials retained")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Confirm(f.ctx, token) }()
	}
	wg.Wait()
	close(results)
	successes, invalid := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, email.ErrToken) {
			invalid++
		} else {
			t.Fatal("confirmation failed unexpectedly")
		}
	}
	if successes != 1 || invalid != 1 {
		t.Fatal("token was not consumed exactly once")
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT email_verified_at IS NOT NULL FROM users WHERE id=$1", u.ID).Scan(&verified); err != nil || !verified {
		t.Fatal("email not verified")
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT email_verified_at IS NOT NULL FROM users WHERE id=$1", other.ID).Scan(&verified); err != nil || verified {
		t.Fatal("other user verified")
	}
	if _, err := f.svc.Authenticate(f.ctx, login.AccessToken); err != nil {
		t.Fatal("verification revoked login")
	}
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal("verified account request failed")
	}
	if err := s.Poll(f.ctx); err != nil || len(inbox.Take()) != 0 {
		t.Fatal("verified account sent again")
	}
}

func TestEmailExpiryAndAtomicRollback(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	inbox := &email.MemorySender{}
	s := mailService(t, f, inbox)
	token := verificationToken(t, s, inbox, f, p)
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_tokens SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1", string(p.UserID)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Confirm(f.ctx, token), email.ErrToken) {
		t.Fatal("expired token accepted")
	}
	_, _, p2 := f.account(t)
	token2 := verificationToken(t, s, inbox, f, p2)
	// COMMIT failure must not mark the user verified or consume the capability.
	_, err := f.admin.Exec(f.ctx, `CREATE FUNCTION fail_email_commit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'test fault'; END$$;
 CREATE CONSTRAINT TRIGGER fail_email_commit AFTER UPDATE ON users DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_email_commit();`)
	if err != nil {
		t.Fatal("fault setup failed")
	}
	cleanup := func() {
		if _, err := f.admin.Exec(f.ctx, "DROP TRIGGER fail_email_commit ON users; DROP FUNCTION fail_email_commit()"); err != nil {
			t.Fatal("fault cleanup failed")
		}
	}
	if s.Confirm(f.ctx, token2) == nil {
		cleanup()
		t.Fatal("failed commit reported success")
	}
	cleanup()
	if err := s.Confirm(f.ctx, token2); err != nil {
		t.Fatal("rolled back capability consumed")
	}
}

type failingEmailSender struct{ calls int }

func (s *failingEmailSender) Send(ctx context.Context, m email.Message) error {
	s.calls++
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("missing deadline")
	}
	return errors.New("private provider response")
}
func TestEmailRetryLeasesAndScrubbing(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	sender := &failingEmailSender{}
	s := mailService(t, f, sender)
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal("request failed")
	}
	for attempt := 1; attempt <= 5; attempt++ {
		if err := s.Poll(f.ctx); err != nil {
			t.Fatal("poll failed", err)
		}
		if sender.calls != attempt {
			t.Fatal("wrong attempt count")
		}
		if err := s.Poll(f.ctx); err != nil || sender.calls != attempt {
			t.Fatal("backoff bypassed")
		}
		if _, err := f.admin.Exec(f.ctx, "UPDATE email_outbox SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE token_hash IN (SELECT token_hash FROM email_tokens WHERE user_id=$1)", string(p.UserID)); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var scrubbed bool
	var attempts int
	if err := f.admin.QueryRow(f.ctx, "SELECT status,link IS NULL AND recipient IS NULL,attempts FROM email_outbox WHERE token_hash IN (SELECT token_hash FROM email_tokens WHERE user_id=$1)", string(p.UserID)).Scan(&status, &scrubbed, &attempts); err != nil || status != "failed" || !scrubbed || attempts != 5 {
		t.Fatal("terminal payload not scrubbed")
	}
	// Recover a worker that died while holding its fifth lease; do not retry forever.
	_, _, p2 := f.account(t)
	if err := s.Request(f.ctx, p2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_outbox SET status='leased',attempts=5,lease_until=clock_timestamp()-interval '1 second' WHERE token_hash IN (SELECT token_hash FROM email_tokens WHERE user_id=$1)", string(p2.UserID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT link IS NULL AND status='failed' FROM email_outbox WHERE token_hash IN (SELECT token_hash FROM email_tokens WHERE user_id=$1)", string(p2.UserID)).Scan(&scrubbed); err != nil || !scrubbed {
		t.Fatal("expired final lease retained secrets")
	}
}

func TestEmailQuotasAndQueueCapacity(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	s := mailService(t, f, &email.MemorySender{})
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Request(f.ctx, p), email.ErrLimited) {
		t.Fatal("cooldown ignored")
	}
	for range 2 {
		if _, err := f.admin.Exec(f.ctx, "UPDATE email_tokens SET created_at=clock_timestamp()-interval '2 minutes' WHERE user_id=$1", string(p.UserID)); err != nil {
			t.Fatal(err)
		}
		if err := s.Request(f.ctx, p); err != nil {
			t.Fatal("daily quota too strict")
		}
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_tokens SET created_at=clock_timestamp()-interval '2 minutes' WHERE user_id=$1", string(p.UserID)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Request(f.ctx, p), email.ErrLimited) {
		t.Fatal("daily quota ignored")
	}
	// Fill to exactly the global cap without producing external messages.
	_, err := f.admin.Exec(f.ctx, `INSERT INTO email_outbox(id,token_hash,recipient,link,expires_at)
 SELECT gen_random_uuid(),(SELECT token_hash FROM email_tokens LIMIT 1),'fake@example.com','secret',clock_timestamp()+interval '1 day'
 FROM generate_series(1,1000-(SELECT count(*)::int FROM email_outbox WHERE status IN ('pending','leased')))`)
	if err != nil {
		t.Fatal("capacity fixture failed")
	}
	t.Cleanup(func() { _, _ = f.admin.Exec(f.ctx, "DELETE FROM email_outbox WHERE recipient='fake@example.com'") })
	_, _, p2 := f.account(t)
	if !errors.Is(s.Request(f.ctx, p2), email.ErrLimited) {
		t.Fatal("queue cap ignored")
	}
}

func TestEmailHTTPAndDisabledIsolation(t *testing.T) {
	f := accountDatabase(t)
	_, login, p := f.account(t)
	inbox := &email.MemorySender{}
	s := mailService(t, f, inbox)
	router := server.NewRouter(server.Dependencies{Mail: s, Sessions: f.svc, Security: httpapi.SecurityOptions{PublicOrigin: "http://127.0.0.1:5173"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	do := func(method, path, body, bearer string, status int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d", method, path, w.Code, status)
		}
	}
	do("POST", "/auth/email/verification", `{"user_id":"someone-else"}`, login.AccessToken, 400)
	do("POST", "/auth/email/verification", "", "", 401)
	token := verificationToken(t, s, inbox, f, p)
	do("GET", "/auth/email/verification/confirm", "", "", 405)
	do("POST", "/auth/email/verification/confirm", `{"token":"`+token+`","purpose":"reset"}`, "", 400)
	do("POST", "/auth/email/verification/confirm", `{"token":"`+token+`"}`, "", 204)
	do("POST", "/auth/email/verification/confirm", `{"token":"`+token+`"}`, "", 400)
	disabled := server.NewRouter(server.Dependencies{})
	for _, path := range []string{"/auth/email/verification", "/auth/email/verification/confirm"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("not json"))
		w := httptest.NewRecorder()
		disabled.ServeHTTP(w, r)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "EMAIL_DISABLED") {
			t.Fatal("disabled endpoint used authentication/database")
		}
	}
}

type gatedEmailSender struct{ started, release chan struct{} }

func (s *gatedEmailSender) Send(ctx context.Context, m email.Message) error {
	close(s.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
		return nil
	}
}
func TestEmailClaimAndConsumedLeaseFence(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	sender := &gatedEmailSender{make(chan struct{}), make(chan struct{})}
	s := mailService(t, f, sender)
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal(err)
	}
	var link string
	if err := f.admin.QueryRow(f.ctx, "SELECT link FROM email_outbox").Scan(&link); err != nil {
		t.Fatal("queued link missing")
	}
	done := make(chan error, 1)
	go func() { done <- s.Poll(f.ctx) }()
	select {
	case <-sender.started:
	case <-f.ctx.Done():
		t.Fatal("worker did not start")
	}
	// A concurrent worker sees no eligible message while the first lease is valid.
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal("second poll failed")
	}
	token := strings.TrimPrefix(link, "https://taskflow.example.com/verify-email#token=")
	if err := s.Confirm(f.ctx, token); err != nil {
		t.Fatal("confirmation blocked by delivery")
	}
	close(sender.release)
	if err := <-done; err != nil {
		t.Fatal("worker acknowledgment failed")
	}
	var scrubbed bool
	if err := f.admin.QueryRow(f.ctx, "SELECT status='failed' AND link IS NULL AND lease_id IS NULL FROM email_outbox").Scan(&scrubbed); err != nil || !scrubbed {
		t.Fatal("old worker undid capability scrubbing")
	}
}

type timeoutEmailSender struct{}

func (timeoutEmailSender) Send(ctx context.Context, m email.Message) error {
	<-ctx.Done()
	return ctx.Err()
}
func TestEmailSenderTimeoutAndExpiryCleanup(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	s := mailService(t, f, timeoutEmailSender{})
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal("timeout not persisted as a retry")
	}
	var retry bool
	if err := f.admin.QueryRow(f.ctx, "SELECT status='pending' AND attempts=1 AND next_attempt_at>clock_timestamp() FROM email_outbox").Scan(&retry); err != nil || !retry {
		t.Fatal("timeout did not back off")
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_tokens SET expires_at=clock_timestamp()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal("expiry cleanup failed")
	}
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_outbox").Scan(&count); err != nil || count != 0 {
		t.Fatal("expired token/payload retained")
	}
}

func TestEmailGlobalQuotaAndPurposeConstraint(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	s := mailService(t, f, &email.MemorySender{})
	_, err := f.admin.Exec(f.ctx, `INSERT INTO email_tokens(token_hash,user_id,purpose,expires_at)
 SELECT decode(md5(g::text)||md5('fixture'||g::text),'hex'),$1,'verify_email',clock_timestamp()+interval '1 day' FROM generate_series(1,30) g`, string(p.UserID))
	if err != nil {
		t.Fatal("global quota setup failed")
	}
	_, _, other := f.account(t)
	if !errors.Is(s.Request(f.ctx, other), email.ErrLimited) {
		t.Fatal("global request budget bypassed")
	}
	if _, err = f.admin.Exec(f.ctx, `INSERT INTO email_tokens(token_hash,user_id,purpose,expires_at) VALUES(decode(repeat('ff',32),'hex'),$1,'unsupported_purpose',clock_timestamp()+interval '1 day')`, string(p.UserID)); err == nil {
		t.Fatal("unsupported token purpose persisted")
	}
}
func TestEmailExpiredLeaseReclaimed(t *testing.T) {
	f := accountDatabase(t)
	_, _, p := f.account(t)
	inbox := &email.MemorySender{}
	s := mailService(t, f, inbox)
	if err := s.Request(f.ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_outbox SET status='leased',attempts=1,lease_id=gen_random_uuid(),lease_until=clock_timestamp()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(f.ctx); err != nil || len(inbox.Take()) != 1 {
		t.Fatal("expired lease not retried")
	}
	var recovered bool
	if err := f.admin.QueryRow(f.ctx, "SELECT status='sent' AND attempts=2 AND link IS NULL AND lease_id IS NULL FROM email_outbox").Scan(&recovered); err != nil || !recovered {
		t.Fatal("recovered lease not completed safely")
	}
}

func TestEmailStatusAvailableWhenDeliveryDisabled(t *testing.T) {
	f := accountDatabase(t)
	u, _, p := f.account(t)
	other, _, otherP := f.account(t)
	if _, err := f.admin.Exec(f.ctx, "UPDATE users SET email_verified_at=clock_timestamp() WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	s := mailService(t, f, nil)
	status, err := s.Status(f.ctx, p)
	if err != nil || status.Enabled || status.VerifiedAt == nil {
		t.Fatal("disabled delivery hid verification status")
	}
	otherStatus, err := s.Status(f.ctx, otherP)
	if err != nil || otherStatus.VerifiedAt != nil || other.ID == u.ID {
		t.Fatal("status crossed users")
	}
}
