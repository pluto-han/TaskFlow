package task

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pluto-han/taskflow/internal/session"
)

func newTestRouter() (*gin.Engine, *MemoryRepository) {
	gin.SetMode(gin.TestMode)

	repo := NewMemoryRepository()
	service := NewService(repo)
	router := gin.New()
	ss, _ := session.NewService(context.Background(), nil, testSessionRepository{}, testPasswords{})
	Mount(router, service, session.NewHandler(ss).RequireSession)

	return router, repo
}

func TestHandlerGetByID(t *testing.T) {
	router, repo := newTestRouter()

	if err := repo.Save(context.Background(), testOwner, &Task{
		ID:       "task-123",
		Title:    "Learn Go",
		Priority: 2,
	}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"/tasks/task-123",
		nil,
	)
	rec := httptest.NewRecorder()

	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var response Task

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if response.ID != "task-123" {
		t.Errorf("expected task-123, got %s", response.ID)
	}
}

func TestHandlerGetByIDNotFound(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(
		http.MethodGet,
		"/tasks/not-exist",
		nil,
	)
	rec := httptest.NewRecorder()

	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(
		rec.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if response.Error.Code != "TASK_NOT_FOUND" {
		t.Errorf(
			"expected TASK_NOT_FOUND, got %s",
			response.Error.Code,
		)
	}
}

func TestHandlerCreate(t *testing.T) {
	router, repo := newTestRouter()

	body := []byte(`{
		"title": "Learn Docker",
		"description": "Containerize TaskFlow",
		"priority": 3
	}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/tasks",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created Task

	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	if created.Title != "Learn Docker" {
		t.Errorf("unexpected title: %q", created.Title)
	}

	expectedLocation := "/tasks/" + created.ID

	if rec.Header().Get("Location") != expectedLocation {
		t.Errorf(
			"expected Location %q, got %q",
			expectedLocation,
			rec.Header().Get("Location"),
		)
	}

	if _, err := repo.FindByID(context.Background(), testOwner, created.ID); err != nil {
		t.Errorf("created task not found in repository: %v", err)
	}
}

func TestHandlerCreateInvalid(t *testing.T) {
	router, _ := newTestRouter()

	body := []byte(`{
		"title": "",
		"priority": 5
	}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/tasks",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerUpdate(t *testing.T) {
	router, repo := newTestRouter()

	if err := repo.Save(context.Background(), testOwner, &Task{
		ID:        "task-123",
		Title:     "Learn Go",
		Priority:  2,
		Completed: true,
	}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	body := []byte(`{"completed": false, "version": 1}`)

	req := httptest.NewRequest(
		http.MethodPatch,
		"/tasks/task-123",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated Task

	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if updated.Completed {
		t.Error("expected completed=false")
	}

	if updated.Title != "Learn Go" {
		t.Error("title should not have changed")
	}

	saved, err := repo.FindByID(context.Background(), testOwner, "task-123")
	if err != nil {
		t.Fatalf("failed to read updated task: %v", err)
	}

	if saved.Completed {
		t.Error("updated value was not persisted")
	}
}

func TestHandlerUpdateInvalid(t *testing.T) {
	router, repo := newTestRouter()

	if err := repo.Save(context.Background(), testOwner, &Task{
		ID:       "task-123",
		Title:    "Learn Go",
		Priority: 2,
	}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/tasks/task-123",
		bytes.NewBufferString(`{"priority": 5, "version": 1}`),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerDelete(t *testing.T) {
	router, repo := newTestRouter()

	if err := repo.Save(context.Background(), testOwner, &Task{
		ID:       "task-123",
		Title:    "Delete me",
		Priority: 1,
	}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodDelete,
		"/tasks/task-123",
		nil,
	)
	rec := httptest.NewRecorder()

	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	if rec.Body.Len() != 0 {
		t.Errorf("expected empty response body, got %q", rec.Body.String())
	}

	getReq := httptest.NewRequest(
		http.MethodGet,
		"/tasks/task-123",
		nil,
	)
	getRec := httptest.NewRecorder()

	getReq.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after deletion, got %d", getRec.Code)
	}
}

func TestHandlerDeleteNotFound(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(
		http.MethodDelete,
		"/tasks/not-exist",
		nil,
	)
	rec := httptest.NewRecorder()

	req.Header.Set("Authorization", "Bearer "+testToken)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}
