package cache_test

import (
	"context"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	infracache "github.com/pluto-han/taskflow/internal/infra/cache"
	redis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

func redisFixture(t *testing.T) (*infracache.Client, infracache.Options, string) {
	t.Helper()
	addr := os.Getenv("TASKFLOW_CACHE_TEST_ADDR")
	if addr == "" {
		t.Skip("run make test-cache for a disposable Redis instance")
	}
	marker := os.Getenv("TASKFLOW_CACHE_TEST_RUN_ID")
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(marker) {
		t.Fatal("isolated loopback Redis and run marker required")
	}
	password, err := os.ReadFile(os.Getenv("TASKFLOW_CACHE_TEST_PASSWORD_FILE"))
	if err != nil {
		t.Fatal("cannot read fixture password")
	}
	options := infracache.Options{Enabled: true, Addr: addr, Username: "taskflow_cache", Password: strings.TrimSpace(string(password))}
	client, err := infracache.New(options)
	if err != nil {
		t.Fatal("fixture client configuration failed")
	}
	t.Cleanup(func() { _ = client.Close() })
	// The runner writes this proof through docker exec before Go can run. Do not
	// create the marker here: a user-provided Redis address is not test ownership.
	proof, err := client.Get(context.Background(), applicationtask.KeyPrefix+"test-run:"+marker)
	if err != nil || string(proof) != marker {
		t.Fatal("disposable Redis ownership proof missing")
	}
	return client, options, marker
}

func TestRedisClientIntegration(t *testing.T) {
	client, options, marker := redisFixture(t)
	ctx := context.Background()
	key := applicationtask.KeyPrefix + "fixture:" + marker
	if err := client.Ping(ctx); err != nil {
		t.Fatal("authenticated handshake/PING failed")
	}
	if _, err := client.Get(ctx, key); !errors.Is(err, applicationtask.ErrMiss) {
		t.Fatal("missing key was not a cache miss")
	}
	if err := client.Set(ctx, key, []byte("fixture-value"), time.Second); err != nil {
		t.Fatal("SET failed")
	}
	value, err := client.Get(ctx, key)
	if err != nil || string(value) != "fixture-value" {
		t.Fatal("GET failed")
	}
	if err := client.Delete(ctx, key); err != nil {
		t.Fatal("DEL failed")
	}
	if _, err := client.Get(ctx, key); !errors.Is(err, applicationtask.ErrMiss) {
		t.Fatal("deleted value remained available")
	}
	if err := client.Set(ctx, key, []byte("short-lived"), 20*time.Millisecond); err != nil {
		t.Fatal("expiring SET failed")
	}
	expiry := time.Now().Add(time.Second)
	for {
		_, err := client.Get(ctx, key)
		if errors.Is(err, applicationtask.ErrMiss) {
			break
		}
		if err != nil || time.Now().After(expiry) {
			t.Fatal("TTL did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
	raw := redis.NewClient(&redis.Options{Addr: options.Addr, Username: options.Username, Password: options.Password,
		Protocol: 2, DisableIdentity: true, MaxRetries: -1, DialerRetries: 1,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}})
	defer raw.Close()
	unauthenticated := redis.NewClient(&redis.Options{Addr: options.Addr, Protocol: 2, DisableIdentity: true,
		MaxRetries: -1, DialerRetries: 1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	defer unauthenticated.Close()
	if err := unauthenticated.Ping(ctx).Err(); err == nil || !strings.Contains(err.Error(), "NOAUTH") {
		t.Fatal("unauthenticated connection was not rejected")
	}
	// Check the server's ACL directly, independently of the wrapper's key checks.
	// Management commands are attempted only after the isolated ownership proof.
	for _, command := range [][]any{
		{"GET", "another-project:key"}, {"SET", "another-project:key", "value"},
		{"CONFIG", "GET", "maxmemory"}, {"FLUSHALL"}, {"KEYS", "*"},
		{"CLIENT", "SETNAME", "unexpected"},
	} {
		if err := raw.Do(ctx, command...).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Fatal("Redis ACL did not reject an unrelated key or management command")
		}
	}
	options.Password = "wrong-fixture-password"
	bad, err := infracache.New(options)
	if err != nil {
		t.Fatal("wrong-password fixture construction failed")
	}
	defer bad.Close()
	start := time.Now()
	if err := bad.Ping(ctx); !errors.Is(err, applicationtask.ErrUnavailable) || time.Since(start) > 250*time.Millisecond {
		t.Fatal("wrong password was not a prompt sanitized failure")
	}
	// The next test runs in a new Go process AFTER the runner restarts Redis.
	if err := client.Set(ctx, applicationtask.KeyPrefix+"restart:"+marker, []byte("must-disappear"), time.Minute); err != nil {
		t.Fatal("restart fixture SET failed")
	}
}

func TestRedisRestartEmpty(t *testing.T) {
	client, _, marker := redisFixture(t)
	if _, err := client.Get(context.Background(), applicationtask.KeyPrefix+"restart:"+marker); !errors.Is(err, applicationtask.ErrMiss) {
		t.Fatal("restart restored cache data despite persistence being disabled")
	}
}
