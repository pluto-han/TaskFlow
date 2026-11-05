package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/session"
)

func todayTime() time.Time { return time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC) }

// The same behavior contract is exercised against memory and real PostgreSQL.
func todayContract(t *testing.T, repo Repository) {
	t.Helper()
	ctx := context.Background()
	now := todayTime()
	svc := NewServiceWithClock(repo, nil, func() time.Time { return now })
	input := CreateTodayTaskRequest{PlanDate: "2026-11-05", Title: " Today ", Priority: 2}
	a, err := svc.CreateToday(ctx, testOwner, input)
	if err != nil {
		t.Fatal(err)
	}
	if a.PlanDate == nil || *a.PlanDate != input.PlanDate || a.CanMove || a.ProjectID != nil || a.Title != "Today" || !a.CreatedAt.Equal(now) {
		t.Fatalf("create: %+v", a)
	}
	b, err := svc.CreateToday(ctx, testOwner, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateToday(ctx, otherOwner, input); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Create(ctx, testOwner, CreateTaskRequest{Title: "Ordinary", Priority: 1, DueDate: &now}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListToday(ctx, testOwner, ListOptions{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" || page.PlanDate != input.PlanDate {
		t.Fatalf("page: %+v %v", page, err)
	}
	next, err := svc.ListToday(ctx, testOwner, ListOptions{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != "" {
		t.Fatalf("next: %+v %v", next, err)
	}
	if _, err = svc.ListToday(ctx, otherOwner, ListOptions{Limit: 1, Cursor: page.NextCursor}); !errors.Is(err, ErrInvalidList) {
		t.Fatalf("foreign cursor: %v", err)
	}
	done := true
	updated, err := svc.Update(ctx, testOwner, a.ID, UpdateTaskRequest{Version: &a.Version, Completed: &done})
	if err != nil || updated.CanMove {
		t.Fatalf("complete: %v", err)
	}
	if _, err = svc.Update(ctx, testOwner, a.ID, UpdateTaskRequest{Version: &a.Version, Completed: &done}); !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("stale update: %v", err)
	}
	pid := "00000000-0000-4000-8000-000000000099"
	if _, err = svc.Update(ctx, testOwner, a.ID, UpdateTaskRequest{Version: &updated.Version, ProjectID: OptionalString{Present: true, Value: &pid}}); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("project move: %v", err)
	}
	for _, id := range []string{a.ID, b.ID} {
		if _, err = svc.GetByID(ctx, otherOwner, id); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("foreign get: %v", err)
		}
		if _, err = svc.Update(ctx, otherOwner, id, UpdateTaskRequest{Version: versionPointer(1), Completed: &done}); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("foreign update: %v", err)
		}
		if err = svc.Delete(ctx, otherOwner, id); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("foreign delete: %v", err)
		}
	}
	pageAll, err := svc.ListToday(ctx, testOwner, ListOptions{})
	if err != nil || len(pageAll.Items) != 2 {
		t.Fatalf("completed disappears: %v %+v", err, pageAll)
	}
	now = now.AddDate(0, 0, 1)
	fresh, err := svc.ListToday(ctx, testOwner, ListOptions{})
	if err != nil || len(fresh.Items) != 0 || fresh.PlanDate != "2026-11-06" {
		t.Fatalf("rollover: %+v %v", fresh, err)
	}
	if _, err = svc.ListToday(ctx, testOwner, ListOptions{Cursor: page.NextCursor}); !errors.Is(err, ErrInvalidList) {
		t.Fatalf("yesterday cursor: %v", err)
	}
	if _, err = svc.CreateToday(ctx, testOwner, input); !errors.Is(err, ErrPlanDayChanged) {
		t.Fatalf("stale date: %v", err)
	}
	input.PlanDate = "2026-11-07"
	if _, err = svc.CreateToday(ctx, testOwner, input); !errors.Is(err, ErrPlanDayChanged) {
		t.Fatalf("future date: %v", err)
	}
	day := "2026-11-05"
	history, err := svc.List(ctx, testOwner, ListOptions{ListFilter: ListFilter{PlanDate: &day}})
	if err != nil || len(history.Items) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	all, err := svc.List(ctx, testOwner, ListOptions{})
	if err != nil || len(all.Items) != 3 {
		t.Fatalf("all tasks: %+v %v", all, err)
	}
	got, err := svc.GetByID(ctx, testOwner, a.ID)
	if err != nil || !got.Completed || got.PlanDate == nil {
		t.Fatalf("history modified: %+v %v", got, err)
	}
	// Mutating a returned pointer must not alter repository state.
	*got.PlanDate = "2000-01-01"
	again, err := svc.GetByID(ctx, testOwner, a.ID)
	if err != nil || *again.PlanDate != day {
		t.Fatal("plan_date alias leaked")
	}
	if err = svc.Delete(ctx, testOwner, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetByID(ctx, testOwner, a.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatal("history delete failed")
	}
}

func TestTodayMemoryContract(t *testing.T) { todayContract(t, NewMemoryRepository()) }

func TestTodayValidationAndCancellation(t *testing.T) {
	svc := NewServiceWithClock(NewMemoryRepository(), nil, todayTime)
	for _, date := range []string{"", "0000-01-01", "2026-02-29", "2026-11-05T00:00:00Z"} {
		if _, err := svc.CreateToday(context.Background(), testOwner, CreateTodayTaskRequest{PlanDate: date, Title: "x", Priority: 1}); !errors.Is(err, ErrInvalidTask) {
			t.Fatalf("date %q: %v", date, err)
		}
	}
	for _, q := range []string{"plan_date=", "plan_date=2026-02-29", "plan_date=2026-11-05&plan_date=2026-11-05"} {
		if _, err := ParseListOptions(q); !errors.Is(err, ErrInvalidList) {
			t.Fatalf("query %s: %v", q, err)
		}
	}
	for _, q := range []string{"plan_date=2026-11-05", "project_id=none"} {
		options, err := ParseListOptions(q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.ListToday(context.Background(), testOwner, options); !errors.Is(err, ErrInvalidList) {
			t.Fatalf("today scope override: %v", err)
		}
	}
	if _, err := svc.ListToday(context.Background(), "", ListOptions{}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err := svc.CreateToday(context.Background(), "", CreateTodayTaskRequest{}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.CreateToday(ctx, testOwner, CreateTodayTaskRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := svc.ListToday(ctx, testOwner, ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestTodayHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := NewMemoryRepository()
	svc := NewServiceWithClock(repo, nil, todayTime)
	ss, err := session.NewService(context.Background(), nil, testSessionRepository{}, testPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	Mount(r, svc, session.NewHandler(ss).RequireSession)
	body := `{"plan_date":"2026-11-05","title":"Today","priority":2}`
	rec := taskRequest(r, "POST", "/tasks/today", body, testToken)
	var item Task
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &item) != nil || rec.Header().Get("Location") != "/tasks/"+item.ID {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	rec = taskRequest(r, "GET", "/tasks/today?limit=1", "", testToken)
	var page TodayPage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Timezone != "Europe/Amsterdam" || page.PlanDate != "2026-11-05" || page.NextDayAt.Format(time.RFC3339) != "2026-11-05T23:00:00Z" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("get %d %s", rec.Code, rec.Body.String())
	}
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", "/tasks/today", "", "", 401}, {"POST", "/tasks/today", body, "", 401},
		{"POST", "/tasks/today", `{"plan_date":"2026-11-04","title":"Old","priority":1}`, testToken, 409},
		{"POST", "/tasks/today", `{"plan_date":"2026-11-06","title":"Future","priority":1}`, testToken, 409},
		{"POST", "/tasks/today", `{"plan_date":"2026-02-29","title":"Bad","priority":1}`, testToken, 400},
		{"POST", "/tasks/today", "null", testToken, 400},
		{"GET", "/tasks/today?plan_date=2026-11-05", "", testToken, 400},
		{"GET", "/tasks/today?project_id=none", "", testToken, 400},
		{"GET", "/tasks?plan_date=2026-11-05", "", testToken, 200},
		{"PATCH", "/tasks/" + item.ID, `{"version":1,"plan_date":null}`, testToken, 400},
		{"PATCH", "/tasks/" + item.ID, `{"version":1,"project_id":"00000000-0000-4000-8000-000000000099"}`, testToken, 400},
	} {
		got := taskRequest(r, tc.method, tc.path, tc.body, tc.token)
		if got.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, got.Code, got.Body.String())
		}
		if tc.status == 409 {
			var e struct{ Error struct{ Code string } }
			_ = json.Unmarshal(got.Body.Bytes(), &e)
			if e.Error.Code != "PLAN_DAY_CHANGED" {
				t.Fatal(got.Body.String())
			}
		}
	}
	for _, field := range []string{"owner_id", "project_id", "recurrence_id", "reminder"} {
		attack := fmt.Sprintf(`{"plan_date":"2026-11-05","title":"x","priority":1,%q:null}`, field)
		if got := taskRequest(r, "POST", "/tasks/today", attack, testToken); got.Code != 400 {
			t.Fatalf("accepted %s", field)
		}
	}
	if got := taskRequest(r, "POST", "/tasks", body, testToken); got.Code != 400 {
		t.Fatal("ordinary create accepted plan_date")
	}
}
