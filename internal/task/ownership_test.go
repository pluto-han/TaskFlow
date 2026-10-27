package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/pluto-han/taskflow/internal/session"
)

// This contract is run against both implementations, bypassing Handler/Service.
func repositoryIsolation(t *testing.T, repo Repository, a, b session.UserID) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().Format("150405.000000000")
	own := &Task{ID: "own-" + suffix, Title: "A", Priority: 2, CreatedAt: time.Now().UTC(), OwnerID: b}
	other := &Task{ID: "other-" + suffix, Title: "B", Priority: 2, CreatedAt: time.Now().UTC()}
	if err := repo.Save(ctx, a, own); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, b, other); err != nil {
		t.Fatal(err)
	}
	if own.OwnerID != a {
		t.Fatal("Save used model owner instead of subject")
	}
	for _, pair := range []struct {
		subject session.UserID
		foreign *Task
	}{{a, other}, {b, own}} {
		if _, err := repo.FindByID(ctx, pair.subject, pair.foreign.ID); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("cross-user get: %v", err)
		}
		forged := *pair.foreign
		forged.OwnerID = pair.subject
		forged.Title = "overwritten"
		for _, version := range []int64{1, 99} {
			forged.Version = version
			if err := repo.Update(ctx, pair.subject, &forged); !errors.Is(err, ErrTaskNotFound) {
				t.Fatalf("cross-user update: %v", err)
			}
		}
		if err := repo.Delete(ctx, pair.subject, pair.foreign.ID); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("cross-user delete: %v", err)
		}
		rows, err := repo.FindPage(ctx, pair.subject, PageQuery{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.OwnerID != pair.subject || row.ID == pair.foreign.ID {
				t.Fatal("cross-user list leak")
			}
		}
	}
	own.OwnerID = b
	own.Title = "Updated"
	if err := repo.Update(ctx, a, own); err != nil {
		t.Fatal(err)
	}
	if own.OwnerID != a || own.Version != 2 {
		t.Fatal("owner/version changed incorrectly")
	}
	own.Version = 1
	if err := repo.Update(ctx, a, own); !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("stale version: %v", err)
	}
	saved, err := repo.FindByID(ctx, b, other.ID)
	if err != nil || saved.Title != "B" || saved.Version != 1 {
		t.Fatal("foreign data changed")
	}
	if err := repo.Delete(ctx, a, own.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(ctx, a, own.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatal("delete failed")
	}
	if _, err := repo.FindPage(ctx, "", PageQuery{Limit: 100}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal("empty owner queried tasks")
	}
	if err := repo.Save(ctx, "", &Task{ID: "anonymous"}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal("empty owner saved task")
	}
}
func TestMemoryOwnership(t *testing.T) {
	repositoryIsolation(t, NewMemoryRepository(), testOwner, otherOwner)
}

func taskRequest(router *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func TestTaskHTTPIsolation(t *testing.T) {
	router, repo := newTestRouter()
	ctx := context.Background()
	for _, owner := range []session.UserID{testOwner, otherOwner} {
		if err := repo.Save(ctx, owner, &Task{ID: string(owner), Title: "private", Priority: 2}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/tasks", ""}, {"GET", "/tasks/" + string(testOwner), ""}, {"POST", "/tasks", `{"title":"New","priority":2}`},
		{"PATCH", "/tasks/" + string(testOwner), `{"title":"Changed","version":1}`}, {"DELETE", "/tasks/" + string(testOwner), ""},
	} {
		for _, token := range []string{"", "broken", strings.Repeat("B", 43)} {
			if rec := taskRequest(router, tc.method, tc.path, tc.body, token); rec.Code != 401 {
				t.Fatalf("anonymous %s %s: %d", tc.method, tc.path, rec.Code)
			}
		}
	}
	for _, tc := range []struct {
		token          string
		owner, foreign session.UserID
	}{{testToken, testOwner, otherOwner}, {otherToken, otherOwner, testOwner}} {
		rec := taskRequest(router, "GET", "/tasks?owner_id="+string(tc.foreign), "", tc.token)
		var tasks []Task
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &tasks) != nil || len(tasks) != 1 || tasks[0].ID != string(tc.owner) {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		for _, method := range []string{"GET", "PATCH", "DELETE"} {
			body := `{"title":"Attack","version":1}`
			foreign := taskRequest(router, method, "/tasks/"+string(tc.foreign), body, tc.token)
			missing := taskRequest(router, method, "/tasks/absent", body, tc.token)
			if foreign.Code != 404 || foreign.Body.String() != missing.Body.String() {
				t.Fatalf("%s: foreign/missing differ", method)
			}
		}
		for _, key := range []string{"owner_id", "user_id"} {
			for _, value := range []string{`"` + string(tc.foreign) + `"`, "null"} {
				for _, method := range []string{"POST", "PATCH"} {
					path := "/tasks"
					body := `{"title":"Attack","priority":2,"` + key + `":` + value + `}`
					if method == "PATCH" {
						path += "/" + string(tc.owner)
						body = `{"title":"Attack","version":1,"` + key + `":` + value + `}`
					}
					if rec := taskRequest(router, method, path, body, tc.token); rec.Code != 400 {
						t.Fatalf("accepted %s %s", method, key)
					}
				}
			}
		}
	}
	rec := taskRequest(router, "POST", "/tasks?owner_id="+string(otherOwner), `{"title":"Mine","priority":2}`, testToken)
	var created Task
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %s", rec.Body.String())
	}
	stored, err := repo.FindByID(ctx, testOwner, created.ID)
	if err != nil || stored.OwnerID != testOwner {
		t.Fatal("query identity spoofing succeeded")
	}
}
func TestHandlerFailsClosedWithoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(NewService(NewMemoryRepository(), nil))
	r.GET("/tasks", h.List)
	r.POST("/tasks", h.Create)
	r.GET("/tasks/:id", h.GetByID)
	r.PATCH("/tasks/:id", h.Update)
	r.DELETE("/tasks/:id", h.Delete)
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		path := "/tasks"
		if method == "PATCH" || method == "DELETE" {
			path += "/any"
		}
		if rec := taskRequest(r, method, path, "{}", testToken); rec.Code != 401 {
			t.Fatalf("unguarded handler %s: %d", method, rec.Code)
		}
	}
}
