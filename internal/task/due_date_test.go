package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/session"
)

func dueDateContract(t *testing.T, repo Repository) {
	t.Helper()
	ctx := context.Background()
	gin.SetMode(gin.TestMode)
	ss, err := session.NewService(ctx, nil, testSessionRepository{}, testPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	Mount(router, NewService(repo), session.NewHandler(ss).RequireSession)
	rec := taskRequest(router, "POST", "/tasks", `{"title":"Overdue allowed","priority":2,"due_date":"2000-01-01T12:00:00Z"}`, testToken)
	var task Task
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &task) != nil || task.DueDate == nil {
		t.Fatalf("past date create: %d %s", rec.Code, rec.Body.String())
	}
	path := "/tasks/" + task.ID
	rec = taskRequest(router, "PATCH", path, `{"title":"Keep deadline","version":1}`, testToken)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	stored, err := repo.FindByID(ctx, testOwner, task.ID)
	if err != nil || stored.DueDate == nil || !stored.DueDate.Equal(*task.DueDate) || stored.Version != 2 {
		t.Fatal("omitted due_date was changed")
	}
	rec = taskRequest(router, "PATCH", path, `{"due_date":null,"version":2}`, testToken)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"due_date":null`) {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	stored, err = repo.FindByID(ctx, testOwner, task.ID)
	if err != nil || stored.DueDate != nil || stored.Version != 3 {
		t.Fatal("clear not persisted")
	}
	rec = taskRequest(router, "PATCH", path, `{"due_date":"1999-12-31T23:30:00.123456789+02:00","version":3}`, testToken)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	expected := time.Date(1999, 12, 31, 21, 30, 0, 123456000, time.UTC)
	stored, err = repo.FindByID(ctx, testOwner, task.ID)
	if err != nil || stored.DueDate == nil || !stored.DueDate.Equal(expected) || stored.Version != 4 {
		t.Fatalf("timezone/precision: %+v %v", stored, err)
	}
	for _, body := range []string{
		`{}`, `{"version":4}`, `{"title":null,"version":4}`, `{"priority":0,"version":4}`, `{"priority":4,"version":4}`,
		`{"due_date":"invalid","version":4}`, `{"due_date":"2025-02-30T00:00:00Z","version":4}`,
		`{"due_date":"2025-01-01","version":4}`, `{"due_date":{},"version":4}`, `{"due_date":123,"version":4}`,
		`{"due_date":"0000-01-01T00:00:00Z","version":4}`, `{"due_date":null}`, `{"due_date":null,"version":0}`,
	} {
		rec := taskRequest(router, "PATCH", path, body, testToken)
		if rec.Code != 400 {
			t.Fatalf("invalid patch %s: %d", body, rec.Code)
		}
	}
	if rec := taskRequest(router, "PATCH", path, `{"due_date":null,"version":3}`, testToken); rec.Code != 409 {
		t.Fatalf("stale clear: %d", rec.Code)
	}
	if rec := taskRequest(router, "PATCH", path, `{"due_date":null,"version":4}`, otherToken); rec.Code != 404 {
		t.Fatalf("foreign clear: %d", rec.Code)
	}
	stored, err = repo.FindByID(ctx, testOwner, task.ID)
	if err != nil || stored.DueDate == nil || !stored.DueDate.Equal(expected) || stored.Version != 4 {
		t.Fatal("rejected update changed stored deadline/version")
	}
	// Clearing an already-null deadline is a valid explicit update.
	for version := 4; version <= 5; version++ {
		rec := taskRequest(router, "PATCH", path, fmt.Sprintf(`{"due_date":null,"version":%d}`, version), testToken)
		if rec.Code != 200 {
			t.Fatal("explicit null must be a nonempty patch")
		}
	}
}

func TestMemoryDueDate(t *testing.T) { dueDateContract(t, NewMemoryRepository()) }

func TestDueDateTriStateDecode(t *testing.T) {
	for _, tc := range []struct {
		body              string
		present, hasValue bool
	}{
		{`{}`, false, false}, {`{"due_date":null}`, true, false}, {`{"due_date":"2025-01-01T00:00:00Z"}`, true, true},
	} {
		var req UpdateTaskRequest
		if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
			t.Fatal(err)
		}
		if req.DueDate.Present != tc.present || (req.DueDate.Value != nil) != tc.hasValue {
			t.Fatalf("three-state parse: %s", tc.body)
		}
	}
}

func TestMemoryDeadlineCopies(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	due := time.Now().UTC().Truncate(time.Microsecond)
	original := due
	task := &Task{ID: "copy", Title: "Copy", Priority: 1, DueDate: &due}
	if err := repo.Save(ctx, testOwner, task); err != nil {
		t.Fatal(err)
	}
	due = due.Add(time.Hour)
	read, err := repo.FindByID(ctx, testOwner, "copy")
	if err != nil || !read.DueDate.Equal(original) {
		t.Fatal("Save retained caller pointer")
	}
	*read.DueDate = read.DueDate.Add(time.Hour)
	again, err := repo.FindByID(ctx, testOwner, "copy")
	if err != nil || !again.DueDate.Equal(original) {
		t.Fatal("read leaked stored pointer")
	}
}
