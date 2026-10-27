package task

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/session"
)

func boolPointer(value bool) *bool { return &value }
func intPointer(value int) *int    { return &value }

// Shared by memory and real PostgreSQL tests, with identical fixed fixtures.
func paginationContract(t *testing.T, repo Repository, owner, other session.UserID) {
	t.Helper()
	ctx := context.Background()
	service := NewService(repo)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	var fixture []Task
	for i := 0; i < 37; i++ {
		task := Task{ID: fmt.Sprintf("page-%02d", i), Title: "Fixture", Priority: i%3 + 1, Completed: i%2 == 0, CreatedAt: base.Add(time.Duration(i/4) * time.Second)}
		if i%3 != 0 {
			due := base.Add(time.Duration(i%7) * time.Hour)
			task.DueDate = &due
		}
		if err := repo.Save(ctx, owner, &task); err != nil {
			t.Fatal(err)
		}
		fixture = append(fixture, task)
	}
	if err := repo.Save(ctx, other, &Task{ID: "foreign-page", Title: "Hidden", Priority: 1, CreatedAt: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	sort.Slice(fixture, func(i, j int) bool {
		if fixture[i].CreatedAt.Equal(fixture[j].CreatedAt) {
			return fixture[i].ID > fixture[j].ID
		}
		return fixture[i].CreatedAt.After(fixture[j].CreatedAt)
	})
	from, before := base.Add(2*time.Hour), base.Add(5*time.Hour)
	tests := []struct {
		name   string
		filter ListFilter
	}{
		{"all", ListFilter{}},
		{"false completed", ListFilter{Completed: boolPointer(false)}},
		{"true completed", ListFilter{Completed: boolPointer(true)}},
		{"priority", ListFilter{Priority: intPointer(2)}},
		{"no deadline", ListFilter{HasDueDate: boolPointer(false)}},
		{"deadline", ListFilter{HasDueDate: boolPointer(true)}},
		{"inclusive from", ListFilter{DueFrom: &from}},
		{"exclusive before", ListFilter{DueBefore: &before}},
		{"combined", ListFilter{Completed: boolPointer(false), Priority: intPointer(2), DueFrom: &from, DueBefore: &before}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := []string{}
			// Independent oracle for the fixed fixture; do not reuse repository filtering.
			for _, task := range fixture {
				f := tc.filter
				if f.Completed != nil && task.Completed != *f.Completed {
					continue
				}
				if f.Priority != nil && task.Priority != *f.Priority {
					continue
				}
				if f.HasDueDate != nil && (task.DueDate != nil) != *f.HasDueDate {
					continue
				}
				if f.DueFrom != nil && (task.DueDate == nil || task.DueDate.Before(*f.DueFrom)) {
					continue
				}
				if f.DueBefore != nil && (task.DueDate == nil || !task.DueDate.Before(*f.DueBefore)) {
					continue
				}
				want = append(want, task.ID)
			}
			for _, limit := range []int{1, 4, 7, 100} {
				got := []string{}
				cursor := ""
				seen := map[string]bool{}
				for pages := 0; ; pages++ {
					if pages > 40 {
						t.Fatal("pagination did not terminate")
					}
					page, err := service.List(ctx, owner, ListOptions{Limit: limit, Cursor: cursor, ListFilter: tc.filter})
					if err != nil {
						t.Fatal(err)
					}
					if len(page.Items) > limit {
						t.Fatal("unbounded result")
					}
					for _, task := range page.Items {
						if task.OwnerID != owner || seen[task.ID] {
							t.Fatal("leak or duplicate")
						}
						seen[task.ID] = true
						got = append(got, task.ID)
					}
					if page.NextCursor == "" {
						break
					}
					cursor = page.NextCursor
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("limit %d: got %v want %v", limit, got, want)
				}
			}
		})
	}
	first, err := service.List(ctx, owner, ListOptions{Limit: 4})
	if err != nil || first.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	if _, err := service.List(ctx, other, ListOptions{Cursor: first.NextCursor}); !errors.Is(err, ErrInvalidList) {
		t.Fatal("accepted foreign user cursor")
	}
	if _, err := service.List(ctx, owner, ListOptions{Cursor: first.NextCursor, ListFilter: ListFilter{Priority: intPointer(2)}}); !errors.Is(err, ErrInvalidList) {
		t.Fatal("accepted changed filter cursor")
	}
	// Deleting the anchor does not invalidate the next page: no lookup dependency.
	anchor := first.Items[len(first.Items)-1]
	if err := repo.Delete(ctx, owner, anchor.ID); err != nil {
		t.Fatal(err)
	}
	next, err := service.List(ctx, owner, ListOptions{Limit: 4, Cursor: first.NextCursor})
	if err != nil || len(next.Items) != 4 || next.Items[0].ID != fixture[4].ID {
		t.Fatal("deleted anchor broke pagination")
	}
	// Newer inserts stay before the cursor and do not shift later pages.
	if err := repo.Save(ctx, owner, &Task{ID: "newest-page", Title: "Newer", Priority: 1, CreatedAt: base.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	next, err = service.List(ctx, owner, ListOptions{Limit: 4, Cursor: first.NextCursor})
	if err != nil || next.Items[0].ID != fixture[4].ID {
		t.Fatal("new insert shifted later page")
	}
	// A modified, unsigned position is not an authorization credential.
	raw, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	var altered taskCursor
	if err := json.Unmarshal(raw, &altered); err != nil {
		t.Fatal(err)
	}
	altered.ID = "foreign-page"
	altered.CreatedAt = base.Add(48 * time.Hour)
	alteredRaw, _ := json.Marshal(altered)
	page, err := service.List(ctx, owner, ListOptions{Limit: 100, Cursor: base64.RawURLEncoding.EncodeToString(alteredRaw)})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Items {
		if row.OwnerID != owner {
			t.Fatal("forged position leaked foreign data")
		}
	}
}

func TestMemoryPagination(t *testing.T) {
	paginationContract(t, NewMemoryRepository(), testOwner, otherOwner)
}

func TestListInputValidation(t *testing.T) {
	for _, query := range []string{
		"limit=0", "limit=-1", "limit=101", "limit=abc", "limit=999999999999999999999", "limit=1&limit=2",
		"completed=1", "completed=", "priority=0", "priority=4", "priority=x", "has_due_date=no",
		"due_from=2025-01-01", "due_before=not-a-date", "due_from=2025-01-03T00:00:00Z&due_before=2025-01-02T00:00:00Z",
		"due_from=2025-01-01T00:00:00Z&due_before=2025-01-01T00:00:00Z", "has_due_date=false&due_from=2025-01-01T00:00:00Z",
		"cursor=", "cursor=" + strings.Repeat("x", 2049), "due_from=%zz",
	} {
		if _, err := ParseListOptions(query); !errors.Is(err, ErrInvalidList) {
			t.Fatalf("accepted %q: %v", query, err)
		}
	}
	options, err := ParseListOptions("completed=false&priority=1&limit=100&due_from=2025-01-01T02:00:00%2B02:00")
	if err != nil || options.Completed == nil || *options.Completed || options.Limit != 100 || options.DueFrom.Hour() != 0 {
		t.Fatalf("valid options: %+v %v", options, err)
	}
	service := NewService(NewMemoryRepository())
	for _, cursor := range []string{"!", base64.RawURLEncoding.EncodeToString([]byte(`{}`)), base64.RawURLEncoding.EncodeToString([]byte(`null`)), strings.Repeat("a", 2049)} {
		if _, err := service.List(context.Background(), testOwner, ListOptions{Cursor: cursor}); !errors.Is(err, ErrInvalidList) {
			t.Fatal("invalid cursor accepted")
		}
	}
	scope, _ := cursorScope(testOwner, ListFilter{})
	for _, value := range []taskCursor{
		{Version: 2, Scope: scope, CursorPosition: CursorPosition{CreatedAt: time.Now(), ID: "a"}},
		{Version: 1, Scope: scope},
		{Version: 1, Scope: scope, CursorPosition: CursorPosition{CreatedAt: time.Now(), ID: "\x00"}},
	} {
		raw, _ := json.Marshal(value)
		if _, err := service.List(context.Background(), testOwner, ListOptions{Cursor: base64.RawURLEncoding.EncodeToString(raw)}); !errors.Is(err, ErrInvalidList) {
			t.Fatal("bad cursor fields accepted")
		}
	}
	page, err := service.List(context.Background(), testOwner, ListOptions{})
	if err != nil || page.Items == nil || page.NextCursor != "" {
		t.Fatal("empty page contract")
	}
}

func TestListHTTPPagination(t *testing.T) {
	router, repo := newTestRouter()
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		if err := repo.Save(ctx, testOwner, &Task{ID: fmt.Sprintf("http-%02d", i), Title: "Task", Priority: 1, CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	first := taskRequest(router, "GET", "/tasks", "", testToken)
	var rows []Task
	if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &rows) != nil || len(rows) != DefaultLimit || first.Header().Get("X-Next-Cursor") == "" {
		t.Fatal("default page contract")
	}
	next := taskRequest(router, "GET", "/tasks?cursor="+url.QueryEscape(first.Header().Get("X-Next-Cursor")), "", testToken)
	if next.Code != 200 || json.Unmarshal(next.Body.Bytes(), &rows) != nil || len(rows) != 5 || next.Header().Get("X-Next-Cursor") != "" {
		t.Fatal("last page contract")
	}
	for _, query := range []string{"limit=101", "cursor=garbage", "priority=4", "completed=invalid", "due_from=bad"} {
		rec := taskRequest(router, "GET", "/tasks?"+query, "", testToken)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "INVALID_QUERY") {
			t.Fatalf("query %s: %d %s", query, rec.Code, rec.Body.String())
		}
	}
}
