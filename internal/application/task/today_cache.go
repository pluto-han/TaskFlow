package applicationtask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	rand "math/rand/v2"
	"strconv"
	"strings"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

const todayCacheVersion = 1

const todayCacheMaxAge = 15 * time.Second

// TodayCacheObserver receives fixed categories only, never keys or identities.
type TodayCacheObserver interface {
	ObserveTodayCache(outcome string)
	ObserveTodayCacheFill(outcome string)
	ObserveTodayCacheOperation(operation string, ok bool, elapsed time.Duration)
}

func (s *Service) WithTodayCacheObserver(observer TodayCacheObserver) *Service {
	s.cacheObserver = observer

	return s
}

func (s *Service) observeCacheRead(outcome string) {
	if s.cacheObserver != nil {
		s.cacheObserver.ObserveTodayCache(outcome)
	}
}

func (s *Service) observeCacheOperation(operation string, err error, started time.Time) {
	if s.cacheObserver != nil {
		s.cacheObserver.ObserveTodayCacheOperation(operation, err == nil || errors.Is(err, ErrMiss), time.Since(started))
	}
}

// WithTodayCache is configured once before serving requests. A nil Store keeps
// the original database-only path.
func (s *Service) WithTodayCache(store Store) *Service {
	s.todayCache = store

	return s
}

func todayCacheEligible(options ListOptions) bool {
	return (options.Limit == 0 || options.Limit == DefaultLimit) && options.Cursor == "" &&
		options.PlanDate == nil && options.ProjectID == nil && options.Completed == nil &&
		options.Priority == nil && options.HasDueDate == nil && options.DueFrom == nil && options.DueBefore == nil
}

func todayCacheKey(owner domainaccount.UserID, date string) string {
	return KeyPrefix + "v" + strconv.Itoa(todayCacheVersion) + ":today:" + string(owner) + ":" + date + ":first20"
}

// InvalidateToday runs only after a confirmed commit. Preserve request values,
// but detach its cancellation and replace its deadline with a short cleanup
// budget. Call synchronously: no unbounded background goroutine is needed.
func (s *Service) InvalidateToday(
	ctx context.Context,
	owner domainaccount.UserID,
	date string,
) error {
	if s.todayCache == nil {
		return nil
	}
	if owner == "" {
		return domainaccount.ErrUnauthorized
	}
	if _, err := domainplanning.ParseDate(date); err != nil {
		return ErrInvalid
	}

	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), OperationTimeout)
	defer cancel()
	started := time.Now()
	err := s.todayCache.Delete(cleanup, todayCacheKey(owner, date))
	s.observeCacheOperation("delete", err, started)

	return err
}

func (s *Service) invalidateTodayAfterWrite(ctx context.Context, actor domainaccount.UserID) {
	if s.todayCache == nil {
		return
	}
	if err := s.InvalidateToday(ctx, actor, domainplanning.At(s.now()).PlanDate); err != nil {
		// Do not include Redis errors, task data, actor IDs or cache keys.
		slog.Warn("today_cache_invalidation_failed")
	}
}

// Task.OwnerID intentionally never appears in public HTTP JSON. Preserve it in
// a private cache envelope, then verify and restore it when decoding.
type cachedTodayItem struct {
	OwnerID domainaccount.UserID `json:"owner_id"`
	Task    cachedTask           `json:"task"`
}

type todayCacheEntry struct {
	Format        int                  `json:"format"`
	OwnerID       domainaccount.UserID `json:"owner_id"`
	PlanDate      string               `json:"plan_date"`
	ReadStartedAt time.Time            `json:"read_started_at"`
	ValidUntil    time.Time            `json:"valid_until"`
	Items         []cachedTodayItem    `json:"items"`
	NextCursor    string               `json:"next_cursor"`
}

func (entry todayCacheEntry) page(
	owner domainaccount.UserID,
	day domainplanning.Day,
) (PageResult, bool) {
	if entry.Format != todayCacheVersion || entry.OwnerID != owner || entry.PlanDate != day.PlanDate ||
		entry.Items == nil || len(entry.Items) > DefaultLimit || entry.ReadStartedAt.IsZero() ||
		entry.ReadStartedAt.After(day.ServerTime) || domainplanning.At(entry.ReadStartedAt).PlanDate != day.PlanDate ||
		!entry.ValidUntil.After(entry.ReadStartedAt) || entry.ValidUntil.After(entry.ReadStartedAt.Add(todayCacheMaxAge)) ||
		entry.ValidUntil.After(day.NextDayAt) || !day.ServerTime.Before(entry.ValidUntil) {
		return PageResult{}, false
	}

	items := make([]TaskResult, 0, len(entry.Items))
	seen := make(map[string]bool, len(entry.Items))
	for _, cached := range entry.Items {
		item := TaskResult(cached.Task)
		if cached.OwnerID != owner || item.PlanDate == nil || *item.PlanDate != day.PlanDate || item.ProjectID != nil ||
			item.ID == "" || len(item.ID) > 128 || strings.ContainsRune(item.ID, 0) || seen[item.ID] ||
			item.CreatedAt.IsZero() || !domaintask.ValidDate(&item.CreatedAt) || !domaintask.ValidDate(item.DueDate) ||
			item.Version < 1 || item.Priority < 1 || item.Priority > 3 || strings.TrimSpace(item.Title) == "" {
			return PageResult{}, false
		}
		if len(items) > 0 {
			previous := items[len(items)-1]
			if item.CreatedAt.After(previous.CreatedAt) || (item.CreatedAt.Equal(previous.CreatedAt) && item.ID >= previous.ID) {
				return PageResult{}, false
			}
		}
		item.OwnerID, item.CanMove = owner, false
		seen[item.ID] = true
		items = append(items, item)
	}
	if entry.NextCursor != "" {
		if len(items) != DefaultLimit {
			return PageResult{}, false
		}

		scope, err := cursorScope(owner, domaintask.ListFilter{PlanDate: &entry.PlanDate})
		if err != nil {
			return PageResult{}, false
		}

		position, err := decodeCursor(entry.NextCursor, scope)
		last := items[len(items)-1]
		if err != nil || position.ID != last.ID || !position.CreatedAt.Equal(last.CreatedAt) {
			return PageResult{}, false
		}
	}
	return PageResult{Items: items, NextCursor: entry.NextCursor}, true
}

func decodeTodayCache(
	raw []byte,
	owner domainaccount.UserID,
	day domainplanning.Day,
) (PageResult, bool) {
	if len(raw) == 0 || len(raw) > MaxValueBytes {
		return PageResult{}, false
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var entry todayCacheEntry
	if decoder.Decode(&entry) != nil || decoder.Decode(new(any)) != io.EOF {
		return PageResult{}, false
	}

	return entry.page(owner, day)
}

func (s *Service) listTodayCached(
	ctx context.Context,
	owner domainaccount.UserID,
	day domainplanning.Day,
) (PageResult, domainplanning.Day, error) {
	key := todayCacheKey(owner, day.PlanDate)
	operationStarted := time.Now()
	raw, readErr := s.todayCache.Get(ctx, key)
	s.observeCacheOperation("get", readErr, operationStarted)
	if err := ctx.Err(); err != nil {
		s.observeCacheRead("error")
		return PageResult{}, day, err
	}

	// Redis itself can straddle midnight. Never use yesterday's result after it.
	current := domainplanning.At(s.now())
	if readErr == nil && current.PlanDate == day.PlanDate {
		if page, ok := decodeTodayCache(raw, owner, current); ok {
			s.observeCacheRead("hit")
			return page, current, nil
		}
	}
	if readErr != nil && !errors.Is(readErr, ErrMiss) {
		s.observeCacheRead("error")
	} else {
		// Missing, expired, malformed or wrong-day bytes all require a DB read.
		s.observeCacheRead("miss")
	}
	// Capture the absolute lifetime BEFORE the database read. A slow read or SET
	// must not grant an old snapshot another full TTL.
	started := s.now()
	day = domainplanning.At(started)
	page, err := s.List(ctx, owner, ListOptions{
		Limit:      DefaultLimit,
		ListFilter: domaintask.ListFilter{PlanDate: &day.PlanDate},
	})
	if err != nil {
		return PageResult{}, day, err
	}
	if err := ctx.Err(); err != nil {
		return PageResult{}, day, err
	}
	// A GET error gets only one cache wait budget: return the DB result without
	// attempting another potentially failing Redis operation in this request.
	if readErr == nil || errors.Is(readErr, ErrMiss) {
		s.fillTodayCache(ctx, owner, day, started, page)
	}
	if err := ctx.Err(); err != nil {
		return PageResult{}, day, err
	}

	return page, day, nil
}

func (s *Service) fillTodayCache(
	ctx context.Context,
	owner domainaccount.UserID,
	day domainplanning.Day,
	started time.Time,
	page PageResult,
) {
	outcome := "skipped"
	defer func() {
		if s.cacheObserver != nil {
			s.cacheObserver.ObserveTodayCacheFill(outcome)
		}
	}()
	ttl := 12*time.Second + time.Duration(rand.Int64N(int64(3*time.Second)+1))
	until := started.Add(ttl)
	if day.NextDayAt.Before(until) {
		until = day.NextDayAt
	}
	entry := todayCacheEntry{Format: todayCacheVersion, OwnerID: owner, PlanDate: day.PlanDate,
		ReadStartedAt: started, ValidUntil: until, NextCursor: page.NextCursor, Items: make([]cachedTodayItem, 0, len(page.Items))}
	for _, item := range page.Items {
		entry.Items = append(entry.Items, cachedTodayItem{OwnerID: item.OwnerID, Task: cachedTask(item)})
	}
	if _, ok := entry.page(owner, domainplanning.At(s.now())); !ok {
		return
	}

	raw, err := json.Marshal(entry)
	if err != nil || len(raw) > MaxValueBytes {
		return
	}

	remaining := until.Sub(s.now())
	if remaining < time.Millisecond {
		return
	}

	// Redis TTL cleans up storage; ValidUntil also guards delayed writes/reads.
	operationStarted := time.Now()
	err = s.todayCache.Set(ctx, todayCacheKey(owner, day.PlanDate), raw, remaining)
	s.observeCacheOperation("set", err, operationStarted)
	outcome = "success"
	if err != nil {
		outcome = "error"
	}
}

type cachedTask struct {
	RecurrenceID *string              `json:"recurrence_id"`
	PlanDate     *string              `json:"plan_date"`
	CanMove      bool                 `json:"can_move"`
	ProjectID    *string              `json:"project_id"`
	OwnerID      domainaccount.UserID `json:"-"`
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	Description  string               `json:"description"`
	Completed    bool                 `json:"completed"`
	Priority     int                  `json:"priority"`
	CreatedAt    time.Time            `json:"created_at"`
	DueDate      *time.Time           `json:"due_date"`
	Version      int64                `json:"version"`
}

const (
	OperationTimeout = 50 * time.Millisecond
	KeyPrefix        = "taskflow:cache:"
	MaxValueBytes    = 256 * 1024
)

var (
	ErrMiss        = errors.New("cache miss")
	ErrUnavailable = errors.New("cache unavailable")
	ErrInvalid     = errors.New("invalid cache input")
)

// Store is the small replaceable boundary used by the Today Service.
type Store interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}
