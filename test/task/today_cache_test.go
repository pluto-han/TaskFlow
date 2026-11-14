package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	interfaceshttpsession "github.com/pluto-han/taskflow/internal/interfaces/http/session"
	interfaceshttptask "github.com/pluto-han/taskflow/internal/interfaces/http/task"
	"github.com/pluto-han/taskflow/test/support/task"
)

// This fake deliberately does not enforce TTL: tests must prove that the Service
// validates the absolute deadline even if Redis still returns expired bytes.
type todayStore struct {
	values              map[string][]byte
	gets, sets, deletes int
	getErr, setErr      error
	deleteErr           error
	getHook             func()
	deleteHook          func(context.Context, string)
	deletedKeys         []string
	lastTTL             time.Duration
}

func (f *todayStore) Get(_ context.Context, key string) ([]byte, error) {
	f.gets++
	if f.getHook != nil {
		f.getHook()
	}
	if f.getErr != nil {
		return nil, f.getErr
	}
	value, ok := f.values[key]
	if !ok {
		return nil, applicationtask.ErrMiss
	}
	return append([]byte(nil), value...), nil
}
func (f *todayStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	f.sets++
	f.lastTTL = ttl
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = append([]byte(nil), value...)
	return nil
}
func (f *todayStore) Delete(ctx context.Context, key string) error {
	f.deletes++
	f.deletedKeys = append(f.deletedKeys, key)
	if f.deleteHook != nil {
		f.deleteHook(ctx, key)
	}
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.values, key)
	return nil
}

type todayCountingRepository struct {
	domaintask.Repository
	reads int
	err   error
	hook  func()
}

func (r *todayCountingRepository) FindPage(ctx context.Context, owner domainaccount.UserID, query domaintask.PageQuery) ([]domaintask.Task, error) {
	r.reads++
	if r.hook != nil {
		r.hook()
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.Repository.FindPage(ctx, owner, query)
}

func todayCacheFixture(t *testing.T, count int) (*applicationtask.Service, *todayCountingRepository, *todayStore, *time.Time) {
	t.Helper()
	now := todayTime()
	repo := &todayCountingRepository{Repository: task.NewMemoryRepository()}
	store := &todayStore{values: map[string][]byte{}}
	svc := applicationtask.NewWithClock(repo, nil, func() time.Time { return now })
	for _, owner := range []domainaccount.UserID{testOwner, otherOwner} {
		for i := 0; i < count; i++ {
			if _, err := svc.CreateToday(context.Background(), owner, applicationtask.CreateTodayInput{
				PlanDate: domainplanning.At(now).PlanDate, Title: fmt.Sprintf("Task %02d", i), Priority: i%3 + 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return svc.WithTodayCache(store), repo, store, &now
}

func readCachedToday(t *testing.T, svc *applicationtask.Service, owner domainaccount.UserID) applicationtask.TodayResult {
	t.Helper()
	page, err := svc.ListToday(context.Background(), owner, applicationtask.ListOptions{Limit: applicationtask.DefaultLimit})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestTodayCacheMissHitExpiryAndIsolation(t *testing.T) {
	svc, repo, store, now := todayCacheFixture(t, 24)
	first := readCachedToday(t, svc, testOwner)
	key := todayCacheKey(testOwner, first.PlanDate)
	if len(first.Items) != 20 || first.NextCursor == "" || repo.reads != 1 || store.gets != 1 || store.sets != 1 {
		t.Fatal("miss did not read and fill the default page")
	}
	if store.lastTTL < 12*time.Second || store.lastTTL > todayCacheMaxAge {
		t.Fatal("TTL outside 12–15 seconds")
	}
	var entry todayCacheEntry
	if err := json.Unmarshal(store.values[key], &entry); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	second := readCachedToday(t, svc, testOwner)
	if repo.reads != 1 || store.sets != 1 || !second.ServerTime.Equal(*now) || second.ServerTime.Equal(first.ServerTime) || second.NextCursor != first.NextCursor {
		t.Fatal("hit queried DB, renewed TTL or replayed server time")
	}
	for _, item := range second.Items {
		if item.OwnerID != testOwner || item.CanMove || item.PlanDate == nil || *item.PlanDate != second.PlanDate {
			t.Fatal("private ownership was not restored correctly")
		}
	}
	firstJSON, _ := json.Marshal(first.Items)
	secondJSON, _ := json.Marshal(second.Items)
	if string(firstJSON) != string(secondJSON) || strings.Contains(string(secondJSON), "owner_id") {
		t.Fatal("cached result changed public task JSON")
	}
	// Returned pointer changes must not mutate the stored JSON.
	*second.Items[0].PlanDate = "2000-01-01"
	if *readCachedToday(t, svc, testOwner).Items[0].PlanDate != first.PlanDate {
		t.Fatal("returned pointer aliased cached data")
	}
	other := readCachedToday(t, svc, otherOwner)
	if repo.reads != 2 || other.Items[0].OwnerID != otherOwner || todayCacheKey(otherOwner, other.PlanDate) == key {
		t.Fatal("different users shared a cache entry")
	}
	*now = entry.ValidUntil
	_ = readCachedToday(t, svc, testOwner)
	if repo.reads != 3 || store.sets != 3 {
		t.Fatal("expired bytes were reused")
	}
}

func TestTodayCacheEmptyAndBypass(t *testing.T) {
	svc, repo, store, _ := todayCacheFixture(t, 0)
	for range 2 {
		page := readCachedToday(t, svc, testOwner)
		if page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
			t.Fatal("empty result did not remain an array")
		}
	}
	if repo.reads != 1 || store.sets != 1 {
		t.Fatal("empty successful list was not cached")
	}
	svc, repo, store, _ = todayCacheFixture(t, 24)
	first := readCachedToday(t, svc, testOwner)
	for _, query := range []string{"limit=5", "completed=false", "priority=2", "has_due_date=false", "due_from=2026-01-01T00:00:00Z", "due_before=2027-01-01T00:00:00Z", "cursor=" + first.NextCursor} {
		options, err := interfaceshttptask.ParseListOptions(query)
		if err != nil {
			t.Fatal(err)
		}
		before := repo.reads
		if _, err := svc.ListToday(context.Background(), testOwner, options); err != nil || repo.reads != before+1 {
			t.Fatal("valid filtered/page request did not bypass", query, err)
		}
	}
	for _, query := range []string{"project_id=none", "plan_date=2026-11-05", "priority=4", "cursor=bad", "limit=101"} {
		options, err := interfaceshttptask.ParseListOptions(query)
		if err == nil {
			_, err = svc.ListToday(context.Background(), testOwner, options)
		}
		if !errors.Is(err, domaintask.ErrInvalidList) {
			t.Fatal("cache bypassed invalid query validation", query)
		}
	}
	if store.gets != 1 || store.sets != 1 {
		t.Fatal("filters or invalid input touched the default cache")
	}
	before := repo.reads
	if _, err := svc.List(context.Background(), testOwner, applicationtask.ListOptions{}); err != nil || repo.reads != before+1 || store.gets != 1 {
		t.Fatal("ordinary task list used Today cache")
	}
	svc.WithTodayCache(nil)
	for range 2 {
		_ = readCachedToday(t, svc, testOwner)
	}
	if repo.reads != before+3 || store.gets != 1 {
		t.Fatal("disabled cache changed database path")
	}
}

func TestTodayCacheRejectsBadPayloads(t *testing.T) {
	for _, name := range []string{"json", "trailing", "oversized", "format", "owner", "date", "item owner", "item date", "project", "deadline", "future read", "null items", "cursor", "unknown field"} {
		t.Run(name, func(t *testing.T) {
			count := 1
			if name == "cursor" {
				count = 24
			}
			svc, repo, store, _ := todayCacheFixture(t, count)
			first := readCachedToday(t, svc, testOwner)
			key := todayCacheKey(testOwner, first.PlanDate)
			var entry todayCacheEntry
			if err := json.Unmarshal(store.values[key], &entry); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "format":
				entry.Format++
			case "owner":
				entry.OwnerID = otherOwner
			case "date":
				entry.PlanDate = "2026-11-04"
			case "item owner":
				entry.Items[0].OwnerID = otherOwner
			case "item date":
				date := "2026-11-04"
				entry.Items[0].Task.PlanDate = &date
			case "project":
				id := "00000000-0000-4000-8000-000000000099"
				entry.Items[0].Task.ProjectID = &id
			case "deadline":
				entry.ValidUntil = entry.ReadStartedAt.Add(time.Minute)
			case "future read":
				entry.ReadStartedAt = entry.ReadStartedAt.Add(time.Second)
			case "null items":
				entry.Items = nil
			case "cursor":
				entry.NextCursor = "bad"
			}
			raw, _ := json.Marshal(entry)
			switch name {
			case "json":
				raw = []byte("{")
			case "trailing":
				raw = append(raw, []byte(` {}`)...)
			case "oversized":
				raw = []byte(strings.Repeat("x", applicationtask.MaxValueBytes+1))
			case "unknown field":
				raw = append([]byte(`{"server_time":"old",`), raw[1:]...)
			}
			store.values[key] = raw
			page := readCachedToday(t, svc, testOwner)
			if repo.reads != 2 || store.sets != 2 || len(page.Items) != min(count, applicationtask.DefaultLimit) || page.Items[0].OwnerID != testOwner {
				t.Fatal("bad payload was accepted or not replaced")
			}
		})
	}
}

func TestTodayCacheFailureAndCancellation(t *testing.T) {
	svc, repo, store, _ := todayCacheFixture(t, 1)
	store.getErr = applicationtask.ErrUnavailable
	if len(readCachedToday(t, svc, testOwner).Items) != 1 || repo.reads != 1 || store.sets != 0 {
		t.Fatal("GET error did not fall back without another cache wait")
	}
	store.getErr, store.setErr = nil, applicationtask.ErrUnavailable
	if len(readCachedToday(t, svc, testOwner).Items) != 1 || repo.reads != 2 || store.sets != 1 {
		t.Fatal("SET error broke successful database read")
	}
	repo.err = errors.New("fixture database failure")
	if _, err := svc.ListToday(context.Background(), testOwner, applicationtask.ListOptions{}); !errors.Is(err, repo.err) || store.sets != 1 {
		t.Fatal("database failure was hidden or cached")
	}
	before := store.gets
	if _, err := svc.ListToday(context.Background(), "", applicationtask.ListOptions{}); !errors.Is(err, domainaccount.ErrUnauthorized) || store.gets != before {
		t.Fatal("missing identity accessed cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.ListToday(ctx, testOwner, applicationtask.ListOptions{}); !errors.Is(err, context.Canceled) || store.gets != before {
		t.Fatal("cancelled request accessed cache")
	}
	ctx, cancel = context.WithCancel(context.Background())
	store.getHook = cancel
	beforeReads := repo.reads
	if _, err := svc.ListToday(ctx, testOwner, applicationtask.ListOptions{}); !errors.Is(err, context.Canceled) || repo.reads != beforeReads {
		t.Fatal("GET cancellation was swallowed")
	}
}

func TestTodayCacheMidnightAndDST(t *testing.T) {
	for _, start := range []string{"2026-11-05T22:59:59Z", "2026-03-28T22:59:59Z", "2026-10-24T21:59:59Z"} {
		t.Run(start, func(t *testing.T) {
			svc, repo, store, now := todayCacheFixture(t, 0)
			*now, _ = time.Parse(time.RFC3339, start)
			first := readCachedToday(t, svc, testOwner)
			if store.lastTTL > time.Second || store.lastTTL <= 0 {
				t.Fatal("TTL crossed midnight")
			}
			oldKey := todayCacheKey(testOwner, first.PlanDate)
			*now = first.NextDayAt
			second := readCachedToday(t, svc, testOwner)
			if first.PlanDate == second.PlanDate || repo.reads != 2 || len(store.values) != 2 || second.Timezone != domainplanning.Timezone {
				t.Fatal("midnight reused yesterday's cache")
			}
			if strings.Contains(start, "03-28") && second.NextDayAt.Sub(*now) != 23*time.Hour {
				t.Fatal("spring day is not 23 hours")
			}
			if strings.Contains(start, "10-24") && second.NextDayAt.Sub(*now) != 25*time.Hour {
				t.Fatal("autumn day is not 25 hours")
			}
			// Deliberately copy yesterday's valid JSON to today's key.
			store.values[todayCacheKey(testOwner, second.PlanDate)] = store.values[oldKey]
			_ = readCachedToday(t, svc, testOwner)
			if repo.reads != 3 {
				t.Fatal("yesterday's payload survived a wrong-key copy")
			}
		})
	}
	svc, repo, store, now := todayCacheFixture(t, 0)
	first := readCachedToday(t, svc, testOwner)
	store.getHook = func() { *now = first.NextDayAt }
	second := readCachedToday(t, svc, testOwner)
	if second.PlanDate == first.PlanDate || repo.reads != 2 || !second.ServerTime.Equal(*now) {
		t.Fatal("Redis read spanning midnight returned yesterday")
	}
}

func TestTodayCacheSlowReadCannotRenewSnapshot(t *testing.T) {
	for _, duration := range []time.Duration{10 * time.Second, 16 * time.Second} {
		svc, repo, store, now := todayCacheFixture(t, 1)
		repo.hook = func() { *now = now.Add(duration) }
		if len(readCachedToday(t, svc, testOwner).Items) != 1 {
			t.Fatal("slow database read lost its result")
		}
		if duration < todayCacheMaxAge {
			if store.sets != 1 || store.lastTTL > todayCacheMaxAge-duration || store.lastTTL <= 0 {
				t.Fatal("slow read did not consume the snapshot's TTL")
			}
		} else if store.sets != 0 {
			t.Fatal("expired snapshot was written to cache")
		}
	}
}

func TestTodayCacheExpiredValueCannotHideDatabaseError(t *testing.T) {
	svc, repo, store, now := todayCacheFixture(t, 1)
	_ = readCachedToday(t, svc, testOwner)
	*now = now.Add(16 * time.Second)
	repo.err = errors.New("fixture database failure")
	if _, err := svc.ListToday(context.Background(), testOwner, applicationtask.ListOptions{}); !errors.Is(err, repo.err) || store.sets != 1 {
		t.Fatal("expired cache hid a database failure or cached the error")
	}
}

func TestTodayCacheHTTPRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, repo, store, _ := todayCacheFixture(t, 1)
	sessions, err := applicationsession.New(context.Background(), nil, testSessionRepository{}, testPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	interfaceshttprouter.RegisterTasks(router, svc, interfaceshttpsession.New(sessions).RequireSession)
	for range 2 {
		response := taskRequest(router, "GET", "/tasks/today?limit=20", "", testToken)
		if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache changed HTTP status or security headers")
		}
	}
	if repo.reads != 1 || store.gets != 2 {
		t.Fatal("HTTP request did not use optional cache")
	}
	for _, token := range []string{"", "invalid-token"} {
		if response := taskRequest(router, "GET", "/tasks/today", "", token); response.Code != 401 || store.gets != 2 {
			t.Fatal("unauthenticated request accessed warm cache")
		}
	}
}

type cacheObservation struct {
	reads, fills []string
	operations   []string
	outcomes     []bool
}

func (o *cacheObservation) ObserveTodayCache(outcome string)     { o.reads = append(o.reads, outcome) }
func (o *cacheObservation) ObserveTodayCacheFill(outcome string) { o.fills = append(o.fills, outcome) }
func (o *cacheObservation) ObserveTodayCacheOperation(operation string, ok bool, elapsed time.Duration) {
	o.operations = append(o.operations, operation)
	o.outcomes = append(o.outcomes, ok)
}

func TestTodayCacheObservationFollowsActualPaths(t *testing.T) {
	svc, _, store, now := todayCacheFixture(t, 1)
	observer := &cacheObservation{}
	svc.WithTodayCacheObserver(observer)
	_ = readCachedToday(t, svc, testOwner) // miss, GET success, SET success
	_ = readCachedToday(t, svc, testOwner) // hit
	if _, err := svc.ListToday(context.Background(), testOwner, applicationtask.ListOptions{Limit: 5}); err != nil {
		t.Fatal(err)
	} // bypass
	store.getErr = applicationtask.ErrUnavailable
	_ = readCachedToday(t, svc, testOwner) // error, DB fallback, no SET
	store.getErr = nil
	store.setErr = applicationtask.ErrUnavailable
	delete(store.values, todayCacheKey(testOwner, domainplanning.At(*now).PlanDate))
	_ = readCachedToday(t, svc, testOwner) // miss, SET error
	store.deleteErr = applicationtask.ErrUnavailable
	_ = svc.InvalidateToday(context.Background(), testOwner, domainplanning.At(*now).PlanDate)
	if fmt.Sprint(observer.reads) != "[miss hit bypass error miss]" || fmt.Sprint(observer.fills) != "[success error]" ||
		fmt.Sprint(observer.operations) != "[get set get get get set delete]" || fmt.Sprint(observer.outcomes) != "[true true true false true false false]" {
		t.Fatalf("wrong observation paths: %+v", observer)
	}
	svc.WithTodayCache(nil)
	_ = readCachedToday(t, svc, testOwner)
	if observer.reads[len(observer.reads)-1] != "bypass" {
		t.Fatal("disabled reads not observed as bypass")
	}
}

func TestTodayCacheSkippedFillAndInvalidRequestMetrics(t *testing.T) {
	svc, repo, _, now := todayCacheFixture(t, 1)
	observer := &cacheObservation{}
	svc.WithTodayCacheObserver(observer)
	repo.hook = func() { *now = now.Add(16 * time.Second) }
	_ = readCachedToday(t, svc, testOwner)
	if fmt.Sprint(observer.fills) != "[skipped]" {
		t.Fatal("expired SQL snapshot not observed as skipped")
	}
	_, _ = svc.ListToday(context.Background(), "", applicationtask.ListOptions{})
	if len(observer.reads) != 1 {
		t.Fatal("unauthenticated request added cache activity")
	}
}

// Fixed keys and envelopes assert compatibility with bytes already stored in Redis.
const todayCacheMaxAge = 15 * time.Second

func todayCacheKey(owner domainaccount.UserID, date string) string {
	return "taskflow:cache:v1:today:" + string(owner) + ":" + date + ":first20"
}

type todayCacheEntry struct {
	Format        int                  `json:"format"`
	OwnerID       domainaccount.UserID `json:"owner_id"`
	PlanDate      string               `json:"plan_date"`
	ReadStartedAt time.Time            `json:"read_started_at"`
	ValidUntil    time.Time            `json:"valid_until"`
	Items         []struct {
		OwnerID domainaccount.UserID            `json:"owner_id"`
		Task    interfaceshttptask.TaskResponse `json:"task"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}
