package notification

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/discord"
	"github.com/pluto-han/taskflow/internal/integration"
	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/recurrence"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/telemetry"
	"github.com/pluto-han/taskflow/internal/testdb"
)

type fakeSender struct {
	mu         sync.Mutex
	calls      int
	nonces     []string
	users      []string
	channels   []string
	credential string
	result     discord.SendResult
	err        error
}

func (f *fakeSender) Send(_ context.Context, channel, nonce, _, userID string) (discord.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.nonces = append(f.nonces, nonce)
	f.users = append(f.users, userID)
	f.channels = append(f.channels, channel)
	return f.result, f.err
}
func (f *fakeSender) CredentialFingerprint() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.credential == "" {
		return "default"
	}
	return f.credential
}
func TestNotificationPostgres(t *testing.T) {
	raw := os.Getenv("TASKFLOW_NOTIFICATION_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run scripts/test_integration.py")
	}
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson34_test" {
		t.Fatal("isolated database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if e := pool.QueryRow(ctx, query, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	owner := session.UserID("00000000-0000-4000-8000-000000000034")
	other := session.UserID("00000000-0000-4000-8000-000000000035")
	exec("INSERT INTO users(id,email,password_hash) VALUES ($1,'n34@example.com','x'),($2,'n35@example.com','x')", string(owner), string(other))
	now := time.Now()
	day := planning.At(now).PlanDate
	service := recurrence.NewServiceWithClock(recurrence.NewPostgresRepository(pool), func() time.Time { return now })
	input := recurrence.CreateRequest{Title: "Test @everyone", Priority: 2, AnchorDate: day, IntervalDays: 1, ReminderEnabled: true}
	if _, e = service.Create(ctx, owner, input); !errors.Is(e, recurrence.ErrInvalid) {
		t.Fatal("unbound reminder accepted", e)
	}
	exec("INSERT INTO discord_bindings(owner_id,guild_id,channel_id,channel_name,discord_user_id,bound_at) VALUES($1,'111','222','test','333',now())", string(owner))
	makeRule := func() string {
		t.Helper()
		rule, e := service.Create(ctx, owner, input)
		if e != nil {
			t.Fatal(e)
		}
		return rule.ID
	}
	worker := recurrence.NewPostgresRepository(pool)
	gen := func() {
		t.Helper()
		if _, e := worker.GenerateBatch(ctx, func() time.Time { return now }); e != nil {
			t.Fatal(e)
		}
	}
	fake := &fakeSender{result: discord.SendResult{Status: 200}}
	metrics := telemetry.New(pool, nil)
	sender := NewService(pool, fake).WithObserver(metrics)
	t.Run("atomic rollback", func(t *testing.T) {
		id := makeRule()
		exec("CREATE FUNCTION fail_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fail'; END $$")
		exec("CREATE TRIGGER fail_outbox BEFORE INSERT ON notification_outbox FOR EACH ROW EXECUTE FUNCTION fail_outbox()")
		if _, e := worker.GenerateBatch(ctx, func() time.Time { return now }); e == nil {
			t.Fatal("failed outbox committed")
		}
		if count("SELECT count(*) FROM tasks WHERE recurrence_id=$1", id) != 0 || count("SELECT count(*) FROM recurrence_occurrences WHERE rule_id=$1", id) != 0 {
			t.Fatal("partial transaction")
		}
		exec("DROP TRIGGER fail_outbox ON notification_outbox")
		exec("DROP FUNCTION fail_outbox()")
		gen()
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1", id) != 1 {
			t.Fatal("missing outbox")
		}
		response := httptest.NewRecorder()
		metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
		if !strings.Contains(response.Body.String(), "taskflow_notification_ready_events 1") {
			t.Fatal("queue scrape did not report the eligible event")
		}
	})
	t.Run("concurrent workers and completed task", func(t *testing.T) {
		id := makeRule()
		gen()
		exec("UPDATE tasks SET completed=true WHERE recurrence_id=$1", id)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); _, e := sender.ProcessOne(ctx); errs <- e }()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		fake.mu.Lock()
		for _, id := range fake.users {
			if id != "333" {
				fake.mu.Unlock()
				t.Fatal("wrong mentioned user")
			}
		}
		fake.mu.Unlock()
		if count("SELECT count(*) FROM notification_outbox WHERE status='sent'") != 2 {
			t.Fatal("lost or repeated messages")
		}
		response := httptest.NewRecorder()
		metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
		for _, expected := range []string{"taskflow_notification_ready_events 0", `taskflow_notification_send_total{outcome="success"} 2`} {
			if !strings.Contains(response.Body.String(), expected) {
				t.Fatal("missing metric", expected)
			}
		}
		a, e := sender.List(ctx, string(owner))
		if e != nil || len(a) != 2 {
			t.Fatal(a, e)
		}
		b, e := sender.List(ctx, string(other))
		if e != nil || len(b) != 0 {
			t.Fatal("cross-user leak", e)
		}
	})
	t.Run("lease fencing", func(t *testing.T) {
		id := makeRule()
		gen()
		a, e := sender.claim(ctx)
		if e != nil || a == nil {
			t.Fatal(e)
		}
		exec("UPDATE notification_outbox SET lease_until=now()-interval '1 second' WHERE id=$1", a.ID)
		b, e := sender.claim(ctx)
		if e != nil || b == nil || a.Token == b.Token {
			t.Fatal(e)
		}
		if e = sender.finish(ctx, a, discord.SendResult{Status: 200}, nil); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='leased'", id) != 1 {
			t.Fatal("old lease wrote result")
		}
		if e = sender.finish(ctx, b, discord.SendResult{Status: 200}, nil); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("retry rate limit and cancel", func(t *testing.T) {
		id := makeRule()
		gen()
		fake.mu.Lock()
		fake.result = discord.SendResult{Status: 429, RetryAfter: time.Minute, Global: true}
		fake.mu.Unlock()
		if _, e := sender.ProcessOne(ctx); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='retrying'", id) != 1 {
			t.Fatal("not retrying")
		}
		id2 := makeRule()
		gen()
		if found, e := sender.ProcessOne(ctx); e != nil || found {
			t.Fatal("global cooldown ignored", found, e)
		}
		// A worker that claimed before another worker's 429 must recheck.
		exec("DELETE FROM notification_cooldowns")
		leased, e := sender.claim(ctx)
		if e != nil || leased == nil {
			t.Fatal(e)
		}
		exec("INSERT INTO notification_cooldowns(scope,until_at) VALUES('global',now()+interval '1 minute')")
		valid, e := sender.preflight(ctx, leased)
		if e != nil || valid {
			t.Fatal("preflight ignored new cooldown", e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE id=$1 AND status='retrying' AND attempts=0", leased.ID) != 1 {
			t.Fatal("cooldown consumed an attempt")
		}
		exec("DELETE FROM notification_cooldowns")
		exec("UPDATE discord_bindings SET version=version+1,channel_id='444' WHERE owner_id=$1", string(owner))
		if found, e := sender.ProcessOne(ctx); e != nil || found {
			t.Fatal(found, e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id IN ($1,$2) AND status='canceled'", id, id2) != 2 {
			t.Fatal("stale binding not canceled")
		}
	})

	t.Run("pause cancels immediately and does not revive on resume", func(t *testing.T) {
		id := makeRule()
		gen()
		rule, e := service.Get(ctx, owner, id)
		if e != nil {
			t.Fatal(e)
		}
		pause := true
		rule, e = service.Update(ctx, owner, id, recurrence.UpdateRequest{Version: rule.Version, Paused: &pause})
		if e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='canceled'", id) != 1 {
			t.Fatal("pause did not cancel")
		}
		pause = false
		if _, e = service.Update(ctx, owner, id, recurrence.UpdateRequest{Version: rule.Version, Paused: &pause}); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='canceled'", id) != 1 {
			t.Fatal("old event revived")
		}
	})
	t.Run("network retry and max attempts", func(t *testing.T) {
		id := makeRule()
		gen()
		fake.mu.Lock()
		fake.err = errors.New("fixture network failure")
		fake.result = discord.SendResult{}
		fake.mu.Unlock()
		if _, e := sender.ProcessOne(ctx); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='retrying' AND last_error='network'", id) != 1 {
			t.Fatal("network failure not retried")
		}
		exec("UPDATE notification_outbox SET attempts=7,next_attempt_at=now()-interval '1 second' WHERE rule_id=$1", id)
		if _, e := sender.ProcessOne(ctx); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='failed' AND last_error='attempts_exhausted'", id) != 1 {
			t.Fatal("unbounded retry")
		}
		fake.mu.Lock()
		fake.err = nil
		fake.mu.Unlock()
	})
	t.Run("unauthorized stops further sending", func(t *testing.T) {
		id := makeRule()
		gen()
		fake.mu.Lock()
		fake.result = discord.SendResult{Status: 401}
		fake.mu.Unlock()
		if _, e := sender.ProcessOne(ctx); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='failed'", id) != 1 {
			t.Fatal("401 not failed")
		}
		id2 := makeRule()
		gen()
		if found, e := sender.ProcessOne(ctx); e != nil || found {
			t.Fatal("invalid token cooldown ignored", found, e)
		}
		fake.mu.Lock()
		fake.credential = "new-token-fixture"
		fake.result = discord.SendResult{Status: 200}
		fake.mu.Unlock()
		rotated := NewService(pool, fake)
		if found, e := rotated.ProcessOne(ctx); e != nil || !found {
			t.Fatal("token rotation did not unblock", found, e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='sent'", id2) != 1 {
			t.Fatal("rotated credential did not send")
		}
		fake.mu.Lock()
		fake.credential = ""
		fake.mu.Unlock()
		exec("DELETE FROM notification_cooldowns")
	})

	t.Run("commit failure retries with same nonce", func(t *testing.T) {
		id := makeRule()
		gen()
		fake.mu.Lock()
		fake.result = discord.SendResult{Status: 200}
		fake.err = nil
		before := len(fake.nonces)
		fake.mu.Unlock()
		exec("CREATE FUNCTION fail_notification_confirm() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='sent' THEN RAISE EXCEPTION 'fail'; END IF; RETURN NEW; END $$")
		exec("CREATE TRIGGER fail_notification_confirm BEFORE UPDATE ON notification_outbox FOR EACH ROW EXECUTE FUNCTION fail_notification_confirm()")
		if _, e := sender.ProcessOne(ctx); e == nil {
			t.Fatal("commit failure ignored")
		}
		exec("DROP TRIGGER fail_notification_confirm ON notification_outbox")
		exec("DROP FUNCTION fail_notification_confirm()")
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='leased'", id) != 1 {
			t.Fatal("lease lost after failure")
		}
		exec("UPDATE notification_outbox SET lease_until=now()-interval '1 second' WHERE rule_id=$1", id)
		if _, e := sender.ProcessOne(ctx); e != nil {
			t.Fatal(e)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if len(fake.nonces) != before+2 || fake.nonces[before] != fake.nonces[before+1] {
			t.Fatal("retry nonce changed")
		}
	})
	t.Run("unbind cancels immediately", func(t *testing.T) {
		id := makeRule()
		gen()
		bindings := integration.NewService(pool, true)
		state, e := bindings.Status(ctx, owner)
		if e != nil {
			t.Fatal(e)
		}
		if e = bindings.Unbind(ctx, owner, state.Version); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='canceled'", id) != 1 {
			t.Fatal("unbind did not cancel")
		}
		exec("UPDATE discord_bindings SET guild_id='111',channel_id='444',channel_name='test',discord_user_id='333',bound_at=now(),version=version+1 WHERE owner_id=$1", string(owner))
	})
	t.Run("server errors retry; bad requests fail", func(t *testing.T) {
		for _, tc := range []struct {
			code int
			want string
		}{{500, "retrying"}, {404, "failed"}, {400, "failed"}} {
			id := makeRule()
			gen()
			fake.mu.Lock()
			fake.result = discord.SendResult{Status: tc.code}
			fake.err = nil
			fake.mu.Unlock()
			if _, e := sender.ProcessOne(ctx); e != nil {
				t.Fatal(e)
			}
			if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status=$2", id, tc.want) != 1 {
				t.Fatalf("status %d not %s", tc.code, tc.want)
			}
			if tc.code == 404 {
				exec("DELETE FROM notification_cooldowns")
			}

		}
	})
	t.Run("expiry and permanent failure", func(t *testing.T) {
		id := makeRule()
		gen()
		exec("UPDATE notification_outbox SET expires_at=now()-interval '1 second' WHERE rule_id=$1", id)
		visible, e := sender.List(ctx, string(owner))
		if e != nil {
			t.Fatal(e)
		}
		seen := false
		for _, event := range visible {
			if event.RuleID == id && event.Status == "expired" {
				seen = true
			}
		}
		if !seen {
			t.Fatal("expired status hidden while worker is stopped")
		}

		if found, e := sender.ProcessOne(ctx); e != nil || found {
			t.Fatal(found, e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='expired'", id) != 1 {
			t.Fatal("not expired")
		}
		id2 := makeRule()
		gen()
		fake.mu.Lock()
		fake.result = discord.SendResult{Status: 403}
		fake.mu.Unlock()
		if _, e := sender.ProcessOne(ctx); e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='failed'", id2) != 1 {
			t.Fatal("403 not permanent")
		}
		id3 := makeRule()
		gen()
		if found, e := sender.ProcessOne(ctx); e != nil || found {
			t.Fatal("403 did not stop channel", found, e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='pending'", id3) != 1 {
			t.Fatal("pending status")
		}
		bindings := integration.NewService(pool, true)
		code, e := bindings.Issue(ctx, owner)
		if e != nil {
			t.Fatal(e)
		}
		_, e = bindings.Bind(ctx, code.Code, integration.Target{InteractionID: "34001", GuildID: "111", ChannelID: "444", ChannelName: "test", DiscordUserID: "333"})
		if e != nil {
			t.Fatal(e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='canceled'", id3) != 1 {
			t.Fatal("rebind did not cancel old event")
		}
		if count("SELECT count(*) FROM notification_cooldowns WHERE scope='channel:444' AND until_at>now()") != 0 {
			t.Fatal("successful reauthorization did not clear permanent failure")
		}
		id4 := makeRule()
		gen()
		fake.mu.Lock()
		fake.result = discord.SendResult{Status: 200}
		fake.mu.Unlock()
		if found, e := sender.ProcessOne(ctx); e != nil || !found {
			t.Fatal("reauthorized channel still blocked", found, e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='sent'", id4) != 1 {
			t.Fatal("reauthorized message not sent")
		}

	})
	t.Run("each owner mentions the user in their own verified binding", func(t *testing.T) {
		exec("INSERT INTO discord_bindings(owner_id,guild_id,channel_id,channel_name,discord_user_id,bound_at) VALUES($1,'444','555','other','777',now())", string(other))
		rule, e := service.Create(ctx, other, input)
		if e != nil {
			t.Fatal(e)
		}
		gen()
		fake.mu.Lock()
		fake.result = discord.SendResult{Status: 200}
		fake.err = nil
		fake.mu.Unlock()
		found, e := sender.ProcessOne(ctx)
		if e != nil || !found {
			t.Fatal("other owner event missing", e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='sent'", rule.ID) != 1 {
			t.Fatal("other owner not sent")
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if len(fake.users) == 0 || fake.users[len(fake.users)-1] != "777" || fake.channels[len(fake.channels)-1] != "555" {
			t.Fatal("mention or channel crossed owners")
		}
	})

	t.Run("invalid stored recipient is canceled instead of sent", func(t *testing.T) {
		exec("UPDATE discord_bindings SET discord_user_id='99999999999999999999' WHERE owner_id=$1", string(owner))
		id := makeRule()
		gen()
		fake.mu.Lock()
		before := fake.calls
		fake.mu.Unlock()
		found, e := sender.ProcessOne(ctx)
		if e != nil || !found {
			t.Fatal(found, e)
		}
		if count("SELECT count(*) FROM notification_outbox WHERE rule_id=$1 AND status='canceled'", id) != 1 {
			t.Fatal("invalid recipient not canceled")
		}
		fake.mu.Lock()
		after := fake.calls
		fake.mu.Unlock()
		if before != after {
			t.Fatal("invalid recipient was sent")
		}
	})

}
