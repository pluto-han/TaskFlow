package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	inframail "github.com/pluto-han/taskflow/internal/infra/mail"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
)

func resetLink(t *testing.T, f *accountFixture, s *mailFixture, inbox *inframail.MemorySender, address string) string {
	t.Helper()
	if err := s.RequestReset(f.ctx, applicationaccount.ResetInput{Email: address}); err != nil {
		t.Fatal("reset request failed", err)
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal("reset mail failed", err)
	}
	messages := inbox.Take()
	if len(messages) != 1 || messages[0].Recipient != address || !strings.HasPrefix(messages[0].Link, "https://taskflow.example.com/reset-password#token=") {
		t.Fatal("incorrect reset message")
	}
	var lifetime bool
	if err := f.admin.QueryRow(f.ctx, "SELECT expires_at-created_at BETWEEN interval '1799 seconds' AND interval '1801 seconds' FROM email_tokens WHERE user_id=(SELECT id FROM users WHERE email=$1) AND purpose='reset_password' ORDER BY created_at DESC LIMIT 1", address).Scan(&lifetime); err != nil || !lifetime {
		t.Fatal("reset lifetime is not thirty minutes")
	}
	return strings.TrimPrefix(messages[0].Link, "https://taskflow.example.com/reset-password#token=")
}
func allowNextReset(t *testing.T, f *accountFixture) {
	t.Helper()
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_reset_requests SET created_at=clock_timestamp()-interval '2 minutes'"); err != nil {
		t.Fatal("quota clock setup failed")
	}
}

func TestPasswordResetLifecycleAndPurposeIsolation(t *testing.T) {
	f := accountDatabase(t)
	u, old, p := f.account(t)
	other, otherLogin, _ := f.account(t)
	_, browserToken, err := f.svc.LoginBrowser(f.ctx, applicationsession.LoginInput{Email: u.Email, Password: accountPassword})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	verifyToken := verificationToken(t, s, inbox, f, p)
	first := resetLink(t, f, s, inbox, u.Email)
	allowNextReset(t, f)
	second := resetLink(t, f, s, inbox, u.Email)
	// Issuing a second link must not invalidate an email already in flight.
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_tokens WHERE purpose='reset_password' AND consumed_at IS NULL").Scan(&count); err != nil || count != 2 {
		t.Fatal("new request invalidated old link")
	}
	if !errors.Is(s.Confirm(f.ctx, first), domainaccount.ErrToken) {
		t.Fatal("reset capability verified email")
	}
	if !errors.Is(s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: verifyToken, NewPassword: "a new password"}), domainaccount.ErrResetToken) {
		t.Fatal("verification capability reset password")
	}
	if err := s.Confirm(f.ctx, verifyToken); err != nil {
		t.Fatal("email verification failed")
	}
	// Verification only cancels verification deliveries, not pending reset capabilities.
	if err := s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: first, NewPassword: "a new password"}); err != nil {
		t.Fatal("reset failed", err)
	}
	for _, token := range []string{first, second} {
		if !errors.Is(s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "another new password"}), domainaccount.ErrResetToken) {
			t.Fatal("reset capability replayed")
		}
	}
	if _, err := f.svc.Authenticate(f.ctx, old.AccessToken); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal("old bearer survived")
	}
	if _, err := f.svc.AuthenticateBrowser(f.ctx, browserToken); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal("old browser session survived")
	}
	if _, err := f.svc.Authenticate(f.ctx, otherLogin.AccessToken); err != nil {
		t.Fatal("other user revoked")
	}
	if _, err := f.svc.Login(f.ctx, applicationsession.LoginInput{Email: u.Email, Password: accountPassword}); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal("old password accepted")
	}
	if _, err := f.svc.Login(f.ctx, applicationsession.LoginInput{Email: u.Email, Password: "a new password"}); err != nil {
		t.Fatal("new password rejected")
	}
	var version int
	var verified bool
	if err := f.admin.QueryRow(f.ctx, "SELECT credentials_version,email_verified_at IS NOT NULL FROM users WHERE id=$1", u.ID).Scan(&version, &verified); err != nil || version != 1 || !verified {
		t.Fatal("credentials not updated atomically")
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT credentials_version FROM users WHERE id=$1", other.ID).Scan(&version); err != nil || version != 0 {
		t.Fatal("other credentials changed")
	}
}

type resetHashGate struct {
	hash    string
	arrived chan struct{}
	release <-chan struct{}
	err     error
}

func (h resetHashGate) HashPassword(ctx context.Context, password string) (string, error) {
	select {
	case h.arrived <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case <-h.release:
		return h.hash, h.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func TestPasswordResetConcurrentConsumption(t *testing.T) {
	f := accountDatabase(t)
	u, _, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	token := resetLink(t, f, s, inbox, u.Email)
	hash, err := (infrapassword.PasswordHasher{}).HashPassword(f.ctx, "concurrent reset password")
	if err != nil {
		t.Fatal(err)
	}
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.WithPasswords(resetHashGate{hash: hash, arrived: arrived, release: release})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "concurrent reset password"})
		}()
	}
	for range 2 {
		waitForPassword(t, f, arrived)
	}
	tx, err := f.admin.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, probe := tx.Exec(f.ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", u.ID)
	_ = tx.Rollback(f.ctx)
	if probe != nil {
		t.Fatal("hashing holds user lock")
	}
	unblock()
	success, invalid := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, domainaccount.ErrResetToken) {
			invalid++
		} else {
			t.Fatal("unexpected reset error", err)
		}
	}
	if success != 1 || invalid != 1 {
		t.Fatal("capability consumed more than once")
	}
}
func TestPasswordResetClosesOldLoginRace(t *testing.T) {
	f := accountDatabase(t)
	for _, kind := range []string{"bearer", "browser"} {
		t.Run(kind, func(t *testing.T) {
			u, _, _ := f.account(t)
			inbox := &inframail.MemorySender{}
			s := mailService(t, f, inbox)
			token := resetLink(t, f, s, inbox, u.Email)
			arrived := make(chan struct{}, 1)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			blocked, err := applicationsession.New(f.ctx, f.users, f.repo, barrierPasswords{arrived: arrived, release: release})
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				token string
				err   error
			}
			done := make(chan result, 1)
			go func() {
				input := applicationsession.LoginInput{Email: u.Email, Password: accountPassword}
				if kind == "browser" {
					_, raw, err := blocked.LoginBrowser(f.ctx, input)
					done <- result{raw, err}
				} else {
					login, err := blocked.Login(f.ctx, input)
					done <- result{login.AccessToken, err}
				}
			}()
			waitForPassword(t, f, arrived)
			if err := s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "password after recovery"}); err != nil {
				t.Fatal("reset failed")
			}
			unblock()
			got := <-done
			if got.token != "" || !errors.Is(got.err, domainaccount.ErrUnauthorized) {
				t.Fatal("old password verification established login")
			}
		})
	}
}
func TestPasswordResetExpiryAndPasswordChangeFence(t *testing.T) {
	f := accountDatabase(t)
	u, _, p := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	token := resetLink(t, f, s, inbox, u.Email)
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.WithPasswords(resetHashGate{hash: "unused-fixture-hash", arrived: arrived, release: release})
	done := make(chan error, 1)
	go func() {
		done <- s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "a reset password"})
	}()
	waitForPassword(t, f, arrived)
	if err := f.svc.ChangePassword(f.ctx, p, applicationsession.PasswordChangeInput{CurrentPassword: accountPassword, NewPassword: "authenticated new password"}); err != nil {
		t.Fatal(err)
	}
	unblock()
	if !errors.Is(<-done, domainaccount.ErrResetToken) {
		t.Fatal("reset ignored credential epoch change")
	}
	if _, err := f.svc.Login(f.ctx, applicationsession.LoginInput{Email: u.Email, Password: "authenticated new password"}); err != nil {
		t.Fatal("stale reset overwrote password")
	}
}
func TestPasswordResetCommitRollback(t *testing.T) {
	f := accountDatabase(t)
	u, login, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	token := resetLink(t, f, s, inbox, u.Email)
	_, err := f.admin.Exec(f.ctx, `CREATE FUNCTION fail_reset_commit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'test fault';END$$;
 CREATE CONSTRAINT TRIGGER fail_reset_commit AFTER UPDATE ON sessions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_reset_commit();`)
	if err != nil {
		t.Fatal("fault setup failed")
	}
	cleanup := func() {
		if _, err := f.admin.Exec(f.ctx, "DROP TRIGGER fail_reset_commit ON sessions; DROP FUNCTION fail_reset_commit()"); err != nil {
			t.Fatal("fault cleanup failed")
		}
	}
	if s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "password after rollback"}) == nil {
		cleanup()
		t.Fatal("failed commit reported success")
	}
	cleanup()
	if _, err := f.svc.Authenticate(f.ctx, login.AccessToken); err != nil {
		t.Fatal("rollback revoked old session")
	}
	if _, err := f.svc.Login(f.ctx, applicationsession.LoginInput{Email: u.Email, Password: accountPassword}); err != nil {
		t.Fatal("rollback changed password")
	}
	if err := s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "password after rollback"}); err != nil {
		t.Fatal("rollback consumed token")
	}
}
func TestPasswordResetHTTPUniformityAndLogs(t *testing.T) {
	f := accountDatabase(t)
	u, _, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	var logs bytes.Buffer
	router := interfaceshttprouter.New(interfaceshttprouter.Dependencies{Mail: s.CapabilityService, Security: interfaceshttpmiddleware.SecurityOptions{PublicOrigin: "https://taskflow.example.com"}, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Host = "untrusted.example"
		r.Header.Set("X-Forwarded-Host", "also-untrusted.example")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d", path, w.Code, want)
		}
		return w
	}
	known := request("POST", "/auth/password/reset/request", `{"email":"`+u.Email+`"}`, 202)
	missing := request("POST", "/auth/password/reset/request", `{"email":"missing@example.com"}`, 202)
	limited := request("POST", "/auth/password/reset/request", `{"email":"`+u.Email+`"}`, 202)
	if known.Body.String() != missing.Body.String() || known.Body.String() != limited.Body.String() {
		t.Fatal("reset enumerates accounts")
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal(err)
	}
	messages := inbox.Take()
	if len(messages) != 1 {
		t.Fatal("missing/throttled account queued mail")
	}
	token := strings.TrimPrefix(messages[0].Link, "https://taskflow.example.com/reset-password#token=")
	request("GET", "/auth/password/reset/confirm?token="+token, "", 405)
	confirmed := request("POST", "/auth/password/reset/confirm", `{"token":"`+token+`","new_password":"password via reset"}`, 204)
	var recovered bool
	if err := f.admin.QueryRow(f.ctx, "SELECT email_verified_at IS NOT NULL AND credentials_version=1 AND NOT EXISTS(SELECT 1 FROM sessions WHERE user_id=users.id AND revoked_at IS NULL) FROM users WHERE id=$1", u.ID).Scan(&recovered); err != nil || !recovered {
		t.Fatal("unverified account was not fully recovered")
	}
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "password via reset") || strings.Contains(logs.String(), u.Email) {
		t.Fatal("reset secrets in logs")
	}
	for _, key := range []string{"Referrer-Policy", "Cache-Control"} {
		if known.Header().Get(key) == "" {
			t.Fatal("missing privacy header")
		}
	}
	if known.Header().Get("Set-Cookie") != "" || confirmed.Header().Get("Set-Cookie") != "" || confirmed.Body.Len() != 0 {
		t.Fatal("reset issued session cookie")
	}
	disabled := interfaceshttprouter.New(interfaceshttprouter.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, path := range []string{"/auth/password/reset/request", "/auth/password/reset/confirm"} {
		w := httptest.NewRecorder()
		disabled.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader("not json")))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "EMAIL_DISABLED") {
			t.Fatal("disabled reset parsed identity")
		}
	}
}

func TestPasswordResetExpiresDuringHashing(t *testing.T) {
	f := accountDatabase(t)
	u, login, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	token := resetLink(t, f, s, inbox, u.Email)
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.WithPasswords(resetHashGate{hash: "unused-after-expiry", arrived: arrived, release: release})
	done := make(chan error, 1)
	go func() {
		done <- s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "a reset password"})
	}()
	waitForPassword(t, f, arrived)
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_tokens SET expires_at=clock_timestamp()-interval '1 second' WHERE purpose='reset_password'"); err != nil {
		t.Fatal("expiry setup failed")
	}
	unblock()
	if !errors.Is(<-done, domainaccount.ErrResetToken) {
		t.Fatal("expiry not rechecked after hashing")
	}
	if _, err := f.svc.Authenticate(f.ctx, login.AccessToken); err != nil {
		t.Fatal("expired reset revoked session")
	}
}
func TestPasswordResetDailyQuotaSurvivesTokenCleanup(t *testing.T) {
	f := accountDatabase(t)
	u, _, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	for range 3 {
		allowNextReset(t, f)
		if err := s.RequestReset(f.ctx, applicationaccount.ResetInput{Email: u.Email}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.admin.Exec(f.ctx, "UPDATE email_tokens SET expires_at=clock_timestamp()-interval '1 second'"); err != nil {
			t.Fatal(err)
		}
		if err := s.Poll(f.ctx); err != nil {
			t.Fatal("cleanup failed")
		}
	}
	allowNextReset(t, f)
	for _, address := range []string{u.Email, "unknown-quota@example.com"} {
		for range 4 {
			allowNextReset(t, f)
			if err := s.RequestReset(f.ctx, applicationaccount.ResetInput{Email: address}); err != nil {
				t.Fatal("quota leaked account state")
			}
		}
	}
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_reset_requests").Scan(&count); err != nil || count != 6 {
		t.Fatal("target quota not retained for known and unknown users")
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_tokens WHERE purpose='reset_password'").Scan(&count); err != nil || count != 0 {
		t.Fatal("daily quota bypassed after cleanup")
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE email_reset_requests SET created_at=clock_timestamp()-interval '25 hours'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(f.ctx); err != nil {
		t.Fatal("request retention cleanup failed")
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_reset_requests").Scan(&count); err != nil || count != 0 {
		t.Fatal("expired target digests retained")
	}
}
func TestPasswordResetHashBusyLeavesCapabilityUsable(t *testing.T) {
	f := accountDatabase(t)
	u, login, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	token := resetLink(t, f, s, inbox, u.Email)
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	close(release)
	s.WithPasswords(resetHashGate{arrived: arrived, release: release, err: infrapassword.ErrBusy})
	if !errors.Is(s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "a valid new password"}), infrapassword.ErrBusy) {
		t.Fatal("busy hash not reported")
	}
	if _, err := f.svc.Authenticate(f.ctx, login.AccessToken); err != nil {
		t.Fatal("busy hash revoked session")
	}
	s.WithPasswords(infrapassword.PasswordHasher{})
	if err := s.ConfirmReset(f.ctx, applicationaccount.ResetConfirmInput{Token: token, NewPassword: "a valid new password"}); err != nil {
		t.Fatal("busy hash consumed capability")
	}
}
func TestPasswordResetGlobalAndQueueCapsAreUniform(t *testing.T) {
	f := accountDatabase(t)
	u, _, _ := f.account(t)
	inbox := &inframail.MemorySender{}
	s := mailService(t, f, inbox)
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO email_reset_requests(target_hash) SELECT decode(md5(g::text)||md5('quota'||g::text),'hex') FROM generate_series(1,30) g`); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{u.Email, "unknown-cap@example.com"} {
		if err := s.RequestReset(f.ctx, applicationaccount.ResetInput{Email: address}); err != nil {
			t.Fatal("global cap leaked state")
		}
	}
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_tokens").Scan(&count); err != nil || count != 0 {
		t.Fatal("global request cap bypassed")
	}
	if _, err := f.admin.Exec(f.ctx, "DELETE FROM email_reset_requests"); err != nil {
		t.Fatal(err)
	}
	// A full delivery queue also returns neutral acceptance for either target.
	if err := s.RequestReset(f.ctx, applicationaccount.ResetInput{Email: u.Email}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO email_outbox(id,token_hash,recipient,link,expires_at)
 SELECT gen_random_uuid(),(SELECT token_hash FROM email_tokens LIMIT 1),'fake@example.com','fixture-only',clock_timestamp()+interval '30 minutes' FROM generate_series(1,999)`); err != nil {
		t.Fatal(err)
	}
	allowNextReset(t, f)
	for _, address := range []string{u.Email, "unknown-cap@example.com"} {
		if err := s.RequestReset(f.ctx, applicationaccount.ResetInput{Email: address}); err != nil {
			t.Fatal("full queue leaked state")
		}
	}
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM email_outbox").Scan(&count); err != nil || count != 1000 {
		t.Fatal("full queue grew")
	}
}
