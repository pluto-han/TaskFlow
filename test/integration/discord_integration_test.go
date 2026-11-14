package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	applicationintegration "github.com/pluto-han/taskflow/internal/application/integration"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
	infraintegration "github.com/pluto-han/taskflow/internal/infra/persistence/integration"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

func TestDiscordPostgresIntegration(t *testing.T) {
	raw := os.Getenv("TASKFLOW_DISCORD_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run scripts/test_integration.py")
	}
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson33_test" {
		t.Fatal("isolated lesson33 database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	a := domainaccount.UserID("00000000-0000-4000-8000-000000000033")
	b := domainaccount.UserID("00000000-0000-4000-8000-000000000034")
	if _, e = pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES ($1,'discord-a@example.com','test'),($2,'discord-b@example.com','test')", string(a), string(b)); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	s := applicationintegration.New(infraintegration.NewWithClock(pool, func() time.Time { return now }), true)
	target := domainintegration.Target{InteractionID: "1001", GuildID: "111", ChannelID: "222", ChannelName: "test-channel", DiscordUserID: "333"}
	status, e := s.Status(ctx, a)
	if e != nil || status.Bound || status.Version != 1 {
		t.Fatal(status, e)
	}
	code, e := s.Issue(ctx, a)
	if e != nil || !applicationintegration.ValidCode(code.Code) {
		t.Fatal(e)
	}
	var stored []byte
	if e = pool.QueryRow(ctx, "SELECT code_hash FROM discord_bindings WHERE owner_id=$1", string(a)).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte(code.Code))
	if string(stored) != string(hash[:]) || string(stored) == code.Code {
		t.Fatal("not stored as hash")
	}
	replacement, e := s.Issue(ctx, a)
	if e != nil || replacement.Code == code.Code {
		t.Fatal(e)
	}
	if _, e = s.Bind(ctx, code.Code, target); e != domainintegration.ErrCode {
		t.Fatal("old code remained valid", e)
	}
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for n := range 2 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			candidate := target
			candidate.InteractionID = []string{"1001", "1002"}[n]
			_, e := s.Bind(ctx, replacement.Code, candidate)
			out <- e
		}(n)
	}
	wg.Wait()
	close(out)
	ok, invalid := 0, 0
	for e := range out {
		if e == nil {
			ok++
		} else if errors.Is(e, domainintegration.ErrCode) {
			invalid++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || invalid != 1 {
		t.Fatal("code consumed twice", ok, invalid)
	}
	status, e = s.Status(ctx, a)
	if e != nil || !status.Bound || status.Version != 2 || *status.ChannelID != "222" {
		t.Fatal(status, e)
	}
	other, e := s.Status(ctx, b)
	if e != nil || other.Bound {
		t.Fatal("binding crossed accounts", other, e)
	}
	if e = s.Unbind(ctx, a, 1); e != domainintegration.ErrConflict {
		t.Fatal("stale unbind accepted", e)
	}
	var receipt string
	if e = pool.QueryRow(ctx, "SELECT interaction_id FROM discord_interactions").Scan(&receipt); e != nil {
		t.Fatal(e)
	}
	target.InteractionID = receipt
	replay, e := applicationintegration.New(infraintegration.New(pool), true).Bind(ctx, replacement.Code, target)
	if e != nil || !replay {
		t.Fatal("restart replay not acknowledged", e)
	}
	if e = s.Unbind(ctx, a, 2); e != nil {
		t.Fatal(e)
	}
	replay, e = s.Bind(ctx, replacement.Code, target)
	if e != nil || !replay {
		t.Fatal(e)
	}
	status, e = s.Status(ctx, a)
	if e != nil || status.Bound || status.Version != 3 {
		t.Fatal("replay resurrected binding", status, e)
	}
	// Third issue succeeds; fourth in this window is limited across service instances.
	pending, e := s.Issue(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = applicationintegration.New(infraintegration.New(pool), true).Issue(ctx, a); e != domainintegration.ErrLimited {
		t.Fatal("rate limit not persistent", e)
	}
	if e = s.Unbind(ctx, a, 3); e != nil {
		t.Fatal(e)
	}
	target.InteractionID = "2001"
	if _, e = s.Bind(ctx, pending.Code, target); e != domainintegration.ErrCode {
		t.Fatal("unbind failed to revoke pending code", e)
	}
	now = now.Add(2 * time.Minute)
	expired, e := s.Issue(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	now = now.Add(11 * time.Minute)
	if _, e = s.Bind(ctx, expired.Code, target); e != domainintegration.ErrCode {
		t.Fatal("expired code accepted", e)
	}
	// The same interaction racing with different users' valid codes must mutate only one account.
	ca, e := s.Issue(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	cb, e := s.Issue(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	out = make(chan error, 2)
	for _, c := range []domainintegration.Code{ca, cb} {
		wg.Add(1)
		go func(c domainintegration.Code) { defer wg.Done(); _, e := s.Bind(ctx, c.Code, target); out <- e }(c)
	}
	wg.Wait()
	close(out)
	for e := range out {
		if e != nil {
			t.Fatal(e)
		}
	}
	sa, _ := s.Status(ctx, a)
	sb, _ := s.Status(ctx, b)
	if sa.Bound == sb.Bound {
		t.Fatal("one interaction bound multiple owners")
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM discord_interactions WHERE interaction_id='2001'").Scan(&count); e != nil || count != 1 {
		t.Fatal(e, count)
	}
}
