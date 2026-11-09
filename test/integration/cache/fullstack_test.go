package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/cache"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/telemetry"
	"github.com/pluto-han/taskflow/internal/testdb"
	"github.com/pluto-han/taskflow/internal/user"
)

type observedStore struct {
	cache.Store
	gets, sets atomic.Int64
}

func (s *observedStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.gets.Add(1)
	return s.Store.Get(ctx, key)
}
func (s *observedStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.sets.Add(1)
	return s.Store.Set(ctx, key, value, ttl)
}

type observedSQL struct {
	task.Repository
	reads atomic.Int64
	gate  func(context.Context) error
}

func (r *observedSQL) FindPage(ctx context.Context, owner session.UserID, query task.PageQuery) ([]task.Task, error) {
	r.reads.Add(1)
	rows, err := r.Repository.FindPage(ctx, owner, query)
	if err == nil && r.gate != nil {
		err = r.gate(ctx)
	}
	return rows, err
}

type observedSessions struct {
	session.Repository
	reads atomic.Int64
}

func (r *observedSessions) FindActive(ctx context.Context, hash [32]byte) (session.Principal, error) {
	r.reads.Add(1)
	return r.Repository.FindActive(ctx, hash)
}

type cacheHTTPFixture struct {
	metrics *telemetry.Metrics
	pool    *pgxpool.Pool
	client  *cache.Client
	store   *observedStore
	repo    *observedSQL
	auth    *observedSessions
	service *task.Service
	server  *httptest.Server
	http    *http.Client
	marker  string
}

func newCacheHTTPFixture(t *testing.T, mode string) *cacheHTTPFixture {
	t.Helper()
	client, options, marker := redisFixture(t)
	raw := os.Getenv("TASKFLOW_CACHE_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run make test-cache for the combined disposable databases")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson41_test" {
		t.Fatal("isolated lesson41 PostgreSQL required")
	}
	testdb.Verify(t, cfg)
	gin.SetMode(gin.TestMode)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal("cannot open full-stack fixture")
	}
	t.Cleanup(pool.Close)
	if mode != "" && mode != "performance" {
		if mode == "wrong-password" {
			options.Password = "wrong-test-password"
		} else if mode == "read-only" {
			options.Username = "taskflow_readonly"
		} else {
			t.Fatal("unknown fixture mode")
		}
		client, err = cache.New(options)
		if err != nil {
			t.Fatal("cannot create fault client")
		}
		t.Cleanup(func() { _ = client.Close() })
	}
	store := &observedStore{Store: client}
	repo := &observedSQL{Repository: task.NewPostgresRepository(pool)}
	projects := project.NewPostgresRepository(pool)
	fixed := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }
	if mode == "performance" {
		started := time.Now()
		clock = func() time.Time { return fixed.Add(time.Since(started)) }
	}
	metrics := telemetry.New(nil, nil)
	metrics.SetCacheEnabled(true)
	tasks := task.NewServiceWithClock(repo, projects, clock).WithTodayCache(store).WithTodayCacheObserver(metrics)
	users := user.NewPostgresRepository(pool)
	sessions := &observedSessions{Repository: session.NewPostgresRepository(pool)}
	sessionService, err := session.NewService(context.Background(), users, sessions, auth.PasswordHasher{})
	if err != nil {
		t.Fatal("cannot prepare sessions")
	}
	router := server.NewRouter(server.Dependencies{Tasks: tasks, Projects: project.NewService(projects),
		Users: user.NewService(users, auth.PasswordHasher{}), Sessions: sessionService, Ping: pool.Ping,
		Metrics: metrics, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(httpClient.CloseIdleConnections)
	return &cacheHTTPFixture{metrics: metrics, pool: pool, client: client, store: store, repo: repo, auth: sessions,
		service: tasks, server: httpServer, http: httpClient, marker: marker}
}

func (f *cacheHTTPFixture) request(t *testing.T, method, path, token string, body any, status int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal("fixture JSON failed")
	}
	request, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal("fixture request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := f.http.Do(request)
	if err != nil {
		t.Fatal("HTTP request failed; credentials omitted")
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != status {
		t.Fatalf("%s %s: got HTTP %d, want %d; body omitted", method, path, response.StatusCode, status)
	}
	return result
}

const fixturePassword = "lesson41 isolated passphrase"

func (f *cacheHTTPFixture) register(t *testing.T, label string) (session.UserID, string, string) {
	t.Helper()
	email := "cache41-" + f.marker[:12] + "-" + label + "@example.com"
	var registered struct{ ID string }
	if json.Unmarshal(f.request(t, "POST", "/auth/register", "", map[string]string{"email": email, "password": fixturePassword}, 201), &registered) != nil {
		t.Fatal("cannot decode registered fixture user")
	}
	return session.UserID(registered.ID), f.login(t, email), email
}

func (f *cacheHTTPFixture) login(t *testing.T, email string) string {
	t.Helper()
	var result session.LoginResponse
	if json.Unmarshal(f.request(t, "POST", "/auth/login", "", map[string]string{"email": email, "password": fixturePassword}, 200), &result) != nil || result.AccessToken == "" {
		t.Fatal("cannot decode fixture login")
	}
	return result.AccessToken
}

func (f *cacheHTTPFixture) today(t *testing.T, token string) task.TodayPage {
	t.Helper()
	var page task.TodayPage
	if json.Unmarshal(f.request(t, "GET", "/tasks/today?limit=20", token, nil, 200), &page) != nil {
		t.Fatal("cannot decode Today response")
	}
	return page
}

func (f *cacheHTTPFixture) create(t *testing.T, token, title string) task.Task {
	t.Helper()
	var item task.Task
	if json.Unmarshal(f.request(t, "POST", "/tasks/today", token,
		map[string]any{"plan_date": "2026-11-05", "title": title, "priority": 2}, 201), &item) != nil || item.ID == "" {
		t.Fatal("cannot decode created task")
	}
	return item
}

func todayFixtureKey(owner session.UserID) string {
	return cache.KeyPrefix + "v1:today:" + string(owner) + ":2026-11-05:first20"
}

func TestRedisPostgresRevocationAndCorruption(t *testing.T) {
	f := newCacheHTTPFixture(t, "")
	owner, token, email := f.register(t, "auth")
	item := f.create(t, token, "Private A")
	_ = f.today(t, token)
	_ = f.today(t, token)
	if f.repo.reads.Load() != 1 {
		t.Fatal("fixture did not warm Redis before logout")
	}
	f.request(t, "POST", "/auth/logout", token, nil, 204)
	cacheReads, authReads := f.store.gets.Load(), f.auth.reads.Load()
	f.request(t, "GET", "/tasks/today?limit=20", token, nil, 401)
	if f.store.gets.Load() != cacheReads || f.auth.reads.Load() != authReads+1 {
		t.Fatal("revoked session reached warm cache or skipped database authentication")
	}
	var revoked int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NOT NULL", string(owner)).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatal("logout did not persist session revocation")
	}
	token = f.login(t, email)
	if page := f.today(t, token); len(page.Items) != 1 || page.Items[0].ID != item.ID || f.repo.reads.Load() != 1 {
		t.Fatal("new session could not reuse the owner's legitimate cache")
	}
	other, otherToken, _ := f.register(t, "peer")
	_ = f.create(t, otherToken, "Private B")
	_ = f.today(t, otherToken)
	foreign, err := f.client.Get(context.Background(), todayFixtureKey(other))
	if err != nil {
		t.Fatal("cannot capture foreign cache fixture")
	}
	for _, bad := range [][]byte{foreign, []byte("{broken JSON")} {
		if err := f.client.Set(context.Background(), todayFixtureKey(owner), bad, time.Second); err != nil {
			t.Fatal("cannot inject owned test cache corruption")
		}
		before := f.repo.reads.Load()
		page := f.today(t, token)
		if f.repo.reads.Load() != before+1 || len(page.Items) != 1 || page.Items[0].ID != item.ID {
			t.Fatal("corruption returned another user's data or failed to fall back")
		}
	}
}

func TestRedisPostgresDatabaseFailure(t *testing.T) {
	f := newCacheHTTPFixture(t, "")
	owner, token, _ := f.register(t, "database")
	_ = f.create(t, token, "Database remains authoritative")
	_ = f.today(t, token)
	key := todayFixtureKey(owner)
	valid, err := f.client.Get(context.Background(), key)
	if err != nil {
		t.Fatal("missing warm fixture")
	}
	var expired map[string]any
	if json.Unmarshal(valid, &expired) != nil {
		t.Fatal("cannot decode owned fixture")
	}
	fixed := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	expired["read_started_at"] = fixed.Add(-20 * time.Second).Format(time.RFC3339Nano)
	expired["valid_until"] = fixed.Add(-5 * time.Second).Format(time.RFC3339Nano)
	bad, err := json.Marshal(expired)
	if err != nil || f.client.Set(context.Background(), key, bad, time.Second) != nil {
		t.Fatal("cannot inject expired owned payload")
	}
	f.pool.Close() // Fail the actual PostgreSQL adapter without touching other fixtures' pools.
	fills := f.store.sets.Load()
	if _, err := f.service.ListToday(context.Background(), owner, task.ListOptions{}); err == nil || f.store.sets.Load() != fills {
		t.Fatal("expired Redis data hid the database error or cached it")
	}
	if err := f.client.Set(context.Background(), key, valid, time.Second); err != nil {
		t.Fatal("cannot restore valid Redis payload")
	}
	cacheReads, authReads := f.store.gets.Load(), f.auth.reads.Load()
	f.request(t, "GET", "/tasks/today?limit=20", token, nil, 500)
	if f.store.gets.Load() != cacheReads || f.auth.reads.Load() != authReads+1 {
		t.Fatal("healthy cache bypassed failed database authentication")
	}
}

// Control commands accept only the runner's exact Redis container and labels.
// No user-supplied container name or arbitrary Docker command is accepted.
func controlRedis(t *testing.T, marker, action string) {
	t.Helper()
	id := os.Getenv("TASKFLOW_CACHE_TEST_CONTAINER_ID")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "docker", "inspect", id).Output()
	var details []struct {
		Name   string
		Config struct{ Labels map[string]string }
		State  struct{ Running, Paused bool }
	}
	if err != nil || json.Unmarshal(raw, &details) != nil || len(details) != 1 ||
		details[0].Name != "/taskflow-cache-test-"+marker+"-redis-1" ||
		details[0].Config.Labels["taskflow.cache-test"] != marker {
		t.Fatal("refusing Docker control without exact disposable ownership")
	}
	var args []string
	switch action {
	case "pause":
		args = []string{"pause", id}
	case "unpause":
		if !details[0].State.Paused {
			return
		}
		args = []string{"unpause", id}
	case "stop":
		args = []string{"stop", "--time", "1", id}
	case "start":
		if details[0].State.Running {
			return
		}
		args = []string{"start", id}
	case "seed":
		args = []string{"exec", id, "sh", "-ec",
			`export REDISCLI_AUTH="$(cat /run/secrets/redis_password)"; exec redis-cli --user taskflow_cache SET "$1" "$2"`,
			"fixture", cache.KeyPrefix + "test-run:" + marker, marker}
	default:
		t.Fatal("unsupported fixture action")
	}
	if err := exec.CommandContext(ctx, "docker", args...).Run(); err != nil {
		t.Fatal("disposable Redis control failed")
	}
}

func TestRedisPostgresFaultRecovery(t *testing.T) {
	f := newCacheHTTPFixture(t, "")
	_, token, _ := f.register(t, "fault")
	item := f.create(t, token, "Before fault")
	_ = f.today(t, token)
	_ = f.today(t, token)
	t.Cleanup(func() { controlRedis(t, f.marker, "unpause"); controlRedis(t, f.marker, "start") })
	controlRedis(t, f.marker, "pause")
	before, fills := f.repo.reads.Load(), f.store.sets.Load()
	started := time.Now()
	page := f.today(t, token)
	if len(page.Items) != 1 || f.repo.reads.Load() != before+1 || f.store.sets.Load() != fills || time.Since(started) > time.Second {
		t.Fatal("unresponsive Redis did not return through PostgreSQL in bounded time")
	}
	t.Logf("paused Redis HTTP fallback: %v", time.Since(started))
	f.request(t, "PATCH", "/tasks/"+item.ID, token, map[string]any{"version": item.Version, "completed": true}, 200)
	_ = f.create(t, token, "Created while Redis paused")
	page = f.today(t, token)
	if len(page.Items) != 2 {
		t.Fatal("writes failed during Redis pause")
	}
	f.request(t, "GET", "/health/ready", "", nil, 200)
	controlRedis(t, f.marker, "unpause")
	controlRedis(t, f.marker, "stop")
	before = f.repo.reads.Load()
	if page := f.today(t, token); len(page.Items) != 2 || f.repo.reads.Load() != before+1 {
		t.Fatal("stopped Redis prevented fallback")
	}
	f.request(t, "DELETE", "/tasks/"+item.ID, token, nil, 204)
	f.request(t, "GET", "/health/ready", "", nil, 200)
	controlRedis(t, f.marker, "start")
	deadline := time.Now().Add(5 * time.Second)
	for f.client.Ping(context.Background()) != nil {
		if time.Now().After(deadline) {
			t.Fatal("same Redis client did not reconnect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Restart deliberately discards all cache data, including the ownership key.
	// Re-seed through the already label-verified container, not via a guessed DSN.
	controlRedis(t, f.marker, "seed")
	before = f.repo.reads.Load()
	if len(f.today(t, token).Items) != 1 || len(f.today(t, token).Items) != 1 || f.repo.reads.Load() != before+1 {
		t.Fatal("restart did not rebuild and hit cache using the same client")
	}
}

func TestRedisPostgresAuthenticationAndWriteFailures(t *testing.T) {
	for _, mode := range []string{"wrong-password", "read-only"} {
		t.Run(mode, func(t *testing.T) {
			f := newCacheHTTPFixture(t, mode)
			_, token, _ := f.register(t, mode)
			_ = f.create(t, token, "Persist despite cache error")
			for range 2 {
				if len(f.today(t, token).Items) != 1 {
					t.Fatal("Redis error broke the successful database response")
				}
			}
			if f.repo.reads.Load() != 2 || (mode == "wrong-password" && f.store.sets.Load() != 0) || (mode == "read-only" && f.store.sets.Load() != 2) {
				t.Fatal("cache read/write error policy changed")
			}
		})
	}
}

func TestRedisPostgresConcurrentMisses(t *testing.T) {
	f := newCacheHTTPFixture(t, "")
	owner, token, _ := f.register(t, "parallel")
	_ = f.create(t, token, "Parallel fixture")
	const requests = 8
	ready := make(chan struct{}, requests)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	f.repo.gate = func(ctx context.Context) error {
		ready <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, requests)
	for range requests {
		go func() {
			page, err := f.service.ListToday(ctx, owner, task.ListOptions{})
			if err == nil && len(page.Items) != 1 {
				err = fmt.Errorf("wrong fixture page")
			}
			results <- err
		}()
	}
	for range requests {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("cold reads did not all reach the SQL barrier")
		}
	}
	releaseOnce.Do(func() { close(release) })
	for range requests {
		if err := <-results; err != nil {
			t.Fatal("concurrent miss failed")
		}
	}
	if f.repo.reads.Load() != requests || f.store.sets.Load() != requests {
		t.Fatal("cold fan-out count changed")
	}
	f.repo.gate = nil
	_ = f.today(t, token)
	if f.repo.reads.Load() != requests {
		t.Fatal("warm request did not hit after concurrent fill")
	}
	t.Logf("cold_requests=%d target_SQL=%d warm_additional_SQL=0; no singleflight", requests, f.repo.reads.Load())
}

func TestRedisPostgresEvictionRecovery(t *testing.T) {
	client, _, marker := redisFixture(t)
	// The runner's candidate sets maxmemory=4mb with the SAME allkeys-lru
	// policy. All pressure keys stay in this random project's owned namespace.
	const keys = 48
	payload := bytes.Repeat([]byte("x"), cache.MaxValueBytes)
	key := func(i int) string { return fmt.Sprintf("%seviction:%s:%d", cache.KeyPrefix, marker, i) }
	t.Cleanup(func() {
		for i := range keys {
			_ = client.Delete(context.Background(), key(i))
		}
		// Eviction may remove the test ownership key too; Docker labels remain
		// authoritative for restoring that fixture proof.
		controlRedis(t, marker, "seed")
	})
	for i := range keys {
		if err := client.Set(context.Background(), key(i), payload, time.Minute); err != nil {
			t.Fatal("cache did not evict under bounded test memory pressure")
		}
	}
	misses := 0
	for i := range keys {
		_, err := client.Get(context.Background(), key(i))
		if err == cache.ErrMiss {
			misses++
		} else if err != nil {
			t.Fatal("eviction caused a cache operation failure")
		}
	}
	if misses == 0 {
		t.Fatal("memory pressure did not evict any entries")
	}
	if err := client.Set(context.Background(), key(0), []byte("rebuilt"), time.Second); err != nil {
		t.Fatal("evicted key could not be rebuilt")
	}
	if value, err := client.Get(context.Background(), key(0)); err != nil || string(value) != "rebuilt" {
		t.Fatal("rebuilt key was unavailable")
	}
	t.Logf("evicted=%d/%d; test maxmemory=4MiB, production=64MiB", misses, keys)
}
