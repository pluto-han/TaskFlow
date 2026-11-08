package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/pluto-han/taskflow/internal/cache"
	"github.com/pluto-han/taskflow/internal/planning"
	"github.com/pluto-han/taskflow/internal/session"
)

const todayCacheVersion = 1
const todayCacheMaxAge = 15 * time.Second

// WithTodayCache is configured once before serving requests. A nil Store keeps
// the original database-only path. Write invalidation follows in Lesson 40.
func (s *Service) WithTodayCache(store cache.Store) *Service {
	s.todayCache = store
	return s
}

func todayCacheEligible(options ListOptions) bool {
	return (options.Limit == 0 || options.Limit == DefaultLimit) && options.Cursor == "" &&
		options.PlanDate == nil && options.ProjectID == nil && options.Completed == nil &&
		options.Priority == nil && options.HasDueDate == nil && options.DueFrom == nil && options.DueBefore == nil
}

func todayCacheKey(owner session.UserID, date string) string {
	return cache.KeyPrefix + "v" + strconv.Itoa(todayCacheVersion) + ":today:" + string(owner) + ":" + date + ":first20"
}

// Task.OwnerID intentionally never appears in public HTTP JSON. Preserve it in
// a private cache envelope, then verify and restore it when decoding.
type cachedTodayItem struct {
	OwnerID session.UserID `json:"owner_id"`
	Task    Task           `json:"task"`
}

type todayCacheEntry struct {
	Format        int               `json:"format"`
	OwnerID       session.UserID    `json:"owner_id"`
	PlanDate      string            `json:"plan_date"`
	ReadStartedAt time.Time         `json:"read_started_at"`
	ValidUntil    time.Time         `json:"valid_until"`
	Items         []cachedTodayItem `json:"items"`
	NextCursor    string            `json:"next_cursor"`
}

func (entry todayCacheEntry) page(owner session.UserID, day planning.Day) (TaskPage, bool) {
	if entry.Format != todayCacheVersion || entry.OwnerID != owner || entry.PlanDate != day.PlanDate ||
		entry.Items == nil || len(entry.Items) > DefaultLimit || entry.ReadStartedAt.IsZero() ||
		entry.ReadStartedAt.After(day.ServerTime) || planning.At(entry.ReadStartedAt).PlanDate != day.PlanDate ||
		!entry.ValidUntil.After(entry.ReadStartedAt) || entry.ValidUntil.After(entry.ReadStartedAt.Add(todayCacheMaxAge)) ||
		entry.ValidUntil.After(day.NextDayAt) || !day.ServerTime.Before(entry.ValidUntil) {
		return TaskPage{}, false
	}
	items := make([]Task, 0, len(entry.Items))
	seen := make(map[string]bool, len(entry.Items))
	for _, cached := range entry.Items {
		item := cached.Task
		if cached.OwnerID != owner || item.PlanDate == nil || *item.PlanDate != day.PlanDate || item.ProjectID != nil ||
			item.ID == "" || len(item.ID) > 128 || strings.ContainsRune(item.ID, 0) || seen[item.ID] ||
			item.CreatedAt.IsZero() || !validDate(&item.CreatedAt) || !validDate(item.DueDate) ||
			item.Version < 1 || item.Priority < 1 || item.Priority > 3 || strings.TrimSpace(item.Title) == "" {
			return TaskPage{}, false
		}
		if len(items) > 0 {
			previous := items[len(items)-1]
			if item.CreatedAt.After(previous.CreatedAt) || (item.CreatedAt.Equal(previous.CreatedAt) && item.ID >= previous.ID) {
				return TaskPage{}, false
			}
		}
		item.OwnerID, item.CanMove = owner, false
		seen[item.ID] = true
		items = append(items, item)
	}
	if entry.NextCursor != "" {
		if len(items) != DefaultLimit {
			return TaskPage{}, false
		}
		scope, err := cursorScope(owner, ListFilter{PlanDate: &entry.PlanDate})
		if err != nil {
			return TaskPage{}, false
		}
		position, err := decodeCursor(entry.NextCursor, scope)
		last := items[len(items)-1]
		if err != nil || position.ID != last.ID || !position.CreatedAt.Equal(last.CreatedAt) {
			return TaskPage{}, false
		}
	}
	return TaskPage{Items: items, NextCursor: entry.NextCursor}, true
}

func decodeTodayCache(raw []byte, owner session.UserID, day planning.Day) (TaskPage, bool) {
	if len(raw) == 0 || len(raw) > cache.MaxValueBytes {
		return TaskPage{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var entry todayCacheEntry
	if decoder.Decode(&entry) != nil || decoder.Decode(new(any)) != io.EOF {
		return TaskPage{}, false
	}
	return entry.page(owner, day)
}

func (s *Service) listTodayCached(ctx context.Context, owner session.UserID, day planning.Day) (TaskPage, planning.Day, error) {
	key := todayCacheKey(owner, day.PlanDate)
	raw, readErr := s.todayCache.Get(ctx, key)
	if err := ctx.Err(); err != nil {
		return TaskPage{}, day, err
	}
	// Redis itself can straddle midnight. Never use yesterday's result after it.
	current := planning.At(s.now())
	if readErr == nil && current.PlanDate == day.PlanDate {
		if page, ok := decodeTodayCache(raw, owner, current); ok {
			return page, current, nil
		}
	}
	// Capture the absolute lifetime BEFORE the database read. A slow read or SET
	// must not grant an old snapshot another full TTL.
	started := s.now()
	day = planning.At(started)
	page, err := s.List(ctx, owner, ListOptions{Limit: DefaultLimit, ListFilter: ListFilter{PlanDate: &day.PlanDate}})
	if err != nil {
		return TaskPage{}, day, err
	}
	if err := ctx.Err(); err != nil {
		return TaskPage{}, day, err
	}
	// A GET error gets only one cache wait budget: return the DB result without
	// attempting another potentially failing Redis operation in this request.
	if readErr == nil || errors.Is(readErr, cache.ErrMiss) {
		s.fillTodayCache(ctx, owner, day, started, page)
	}
	if err := ctx.Err(); err != nil {
		return TaskPage{}, day, err
	}
	return page, day, nil
}

func (s *Service) fillTodayCache(ctx context.Context, owner session.UserID, day planning.Day, started time.Time, page TaskPage) {
	ttl := 12*time.Second + time.Duration(rand.Int64N(int64(3*time.Second)+1))
	until := started.Add(ttl)
	if day.NextDayAt.Before(until) {
		until = day.NextDayAt
	}
	entry := todayCacheEntry{Format: todayCacheVersion, OwnerID: owner, PlanDate: day.PlanDate,
		ReadStartedAt: started, ValidUntil: until, NextCursor: page.NextCursor, Items: make([]cachedTodayItem, 0, len(page.Items))}
	for _, item := range page.Items {
		entry.Items = append(entry.Items, cachedTodayItem{OwnerID: item.OwnerID, Task: item})
	}
	if _, ok := entry.page(owner, planning.At(s.now())); !ok {
		return
	}
	raw, err := json.Marshal(entry)
	if err != nil || len(raw) > cache.MaxValueBytes {
		return
	}
	remaining := until.Sub(s.now())
	if remaining < time.Millisecond {
		return
	}
	// Redis TTL cleans up storage; ValidUntil also guards delayed writes/reads.
	_ = s.todayCache.Set(ctx, todayCacheKey(owner, day.PlanDate), raw, remaining)
}
