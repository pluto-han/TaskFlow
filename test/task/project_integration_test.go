package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	interfaceshttpproject "github.com/pluto-han/taskflow/internal/interfaces/http/project"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	interfaceshttpsession "github.com/pluto-han/taskflow/internal/interfaces/http/session"
	interfaceshttptask "github.com/pluto-han/taskflow/internal/interfaces/http/task"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

func TestProjectIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_PROJECT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TASKFLOW_PROJECT_TEST_DATABASE_URL to an isolated migrated taskflow_lesson16_test database")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson16_test" {
		t.Fatal("requires isolated loopback taskflow_lesson16_test")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatal("requires clean migration 15")
	}
	_, err = pool.Exec(ctx, "INSERT INTO users(id,email,password_hash) VALUES ($1,'project-a@example.com','test-only'),($2,'project-b@example.com','test-only')", string(testOwner), string(otherOwner))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, table := range []string{"tasks", "projects", "users"} {
			column := "owner_id"
			if table == "users" {
				column = "id"
			}
			if _, err := pool.Exec(clean, "DELETE FROM "+table+" WHERE "+column+" IN ($1,$2)", string(testOwner), string(otherOwner)); err != nil {
				t.Error(err)
			}
		}
	}()
	pr := infraproject.New(pool)
	ps := applicationproject.New(pr, pr)
	tr := infratask.New(pool)
	ts := applicationtask.New(tr, pr)
	ss, err := applicationsession.New(ctx, nil, testSessionRepository{}, testPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	auth := interfaceshttpsession.New(ss).RequireSession
	interfaceshttprouter.RegisterProjects(router, ps, auth)
	interfaceshttprouter.RegisterTasks(router, ts, auth)
	request := func(method, path, body, token string, status int) []byte {
		t.Helper()
		rec := taskRequest(router, method, path, body, token)
		if rec.Code != status {
			t.Fatalf("%s %s: want %d got %d: %s", method, path, status, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		path := "/projects"
		if method == "PATCH" || method == "DELETE" {
			path += "/00000000-0000-4000-8000-000000000003"
		}
		request(method, path, `{}`, "", 401)
	}
	if string(request("GET", "/projects", "", testToken, 200)) != "[]" {
		t.Fatal("empty list must be []")
	}
	for _, body := range []string{`{}`, `{"name":" "}`, `{"name":"valid","owner_id":"spoof"}`, `{"name":"valid"} {}`, `null`, `{"name":"` + strings.Repeat("x", 65536) + `"}`} {
		request("POST", "/projects", body, testToken, 400)
	}
	createProject := func(token string) interfaceshttpproject.ProjectResponse {
		t.Helper()
		var p interfaceshttpproject.ProjectResponse
		body := request("POST", "/projects", `{"name":"  Project  ","description":"original"}`, token, 201)
		if json.Unmarshal(body, &p) != nil || p.Name != "Project" || strings.Contains(string(body), "owner_id") {
			t.Fatal("bad project response")
		}
		return p
	}
	a, b, foreign := createProject(testToken), createProject(testToken), createProject(otherToken)
	var list []interfaceshttpproject.ProjectResponse
	if json.Unmarshal(request("GET", "/projects?owner_id="+string(otherOwner), "", testToken, 200), &list) != nil || len(list) != 2 {
		t.Fatal("list isolation")
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		one := request(method, "/projects/"+foreign.ID, `{"name":"attack"}`, testToken, 404)
		two := request(method, "/projects/00000000-0000-4000-8000-000000000099", `{"name":"attack"}`, testToken, 404)
		if string(one) != string(two) {
			t.Fatal("foreign project disclosed")
		}
	}
	request("GET", "/projects/bad", "", testToken, 400)
	request("PATCH", "/projects/"+a.ID, `{}`, testToken, 400)
	var updated interfaceshttpproject.ProjectResponse
	if json.Unmarshal(request("PATCH", "/projects/"+a.ID, `{"name":"Renamed"}`, testToken, 200), &updated) != nil || updated.Description != "original" {
		t.Fatal("partial update lost field")
	}
	for _, tc := range []struct {
		id     string
		status int
	}{{"bad", 400}, {foreign.ID, 404}, {"00000000-0000-4000-8000-000000000099", 404}} {
		request("POST", "/tasks", fmt.Sprintf(`{"title":"task","priority":1,"project_id":%q}`, tc.id), testToken, tc.status)
	}
	var item interfaceshttptask.TaskResponse
	if json.Unmarshal(request("POST", "/tasks", fmt.Sprintf(`{"title":"task","priority":1,"project_id":%q}`, a.ID), testToken, 201), &item) != nil || item.ProjectID == nil || *item.ProjectID != a.ID {
		t.Fatal("assignment failed")
	}
	// Project filtering must happen before SQL pagination, with owner scope intact.
	var filtered []interfaceshttptask.TaskResponse
	if json.Unmarshal(request("GET", "/tasks?project_id="+a.ID+"&limit=1", "", testToken, 200), &filtered) != nil || len(filtered) != 1 || filtered[0].ID != item.ID {
		t.Fatal("SQL project filter")
	}
	for _, value := range []string{foreign.ID, "none"} {
		if json.Unmarshal(request("GET", "/tasks?project_id="+value, "", testToken, 200), &filtered) != nil || len(filtered) != 0 {
			t.Fatal("SQL project isolation/unclassified")
		}
	}
	request("GET", "/tasks?project_id=bad", "", testToken, 400)
	second, err := ts.Create(ctx, testOwner, applicationtask.CreateInput{Title: "second project task", Priority: 1, ProjectID: &a.ID})
	if err != nil {
		t.Fatal(err)
	}
	firstPage, err := ts.List(ctx, testOwner, applicationtask.ListOptions{Limit: 1, ListFilter: domaintask.ListFilter{ProjectID: &a.ID}})
	if err != nil || len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("SQL filtered pagination: %v", err)
	}
	nextPage, err := ts.List(ctx, testOwner, applicationtask.ListOptions{Limit: 1, Cursor: firstPage.NextCursor, ListFilter: domaintask.ListFilter{ProjectID: &a.ID}})
	if err != nil || len(nextPage.Items) != 1 || nextPage.NextCursor != "" || firstPage.Items[0].ID == nextPage.Items[0].ID {
		t.Fatal("SQL filtered next page")
	}
	if _, err := ts.List(ctx, testOwner, applicationtask.ListOptions{Limit: 1, Cursor: firstPage.NextCursor, ListFilter: domaintask.ListFilter{ProjectID: &b.ID}}); !errors.Is(err, domaintask.ErrInvalidList) {
		t.Fatal("cross-project cursor accepted")
	}
	if err := tr.Delete(ctx, testOwner, second.ID); err != nil {
		t.Fatal(err)
	}
	unclassified, err := ts.Create(ctx, testOwner, applicationtask.CreateInput{Title: "unclassified", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	none := "none"
	page, err := ts.List(ctx, testOwner, applicationtask.ListOptions{ListFilter: domaintask.ListFilter{ProjectID: &none}})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != unclassified.ID {
		t.Fatal("SQL unclassified filter")
	}
	if err := tr.Delete(ctx, testOwner, unclassified.ID); err != nil {
		t.Fatal(err)
	}
	foreignTask, err := ts.Create(ctx, otherOwner, applicationtask.CreateInput{Title: "private", Priority: 1, ProjectID: &foreign.ID})
	if err != nil {
		t.Fatal(err)
	}
	page, err = ts.List(ctx, testOwner, applicationtask.ListOptions{ListFilter: domaintask.ListFilter{ProjectID: &foreign.ID}})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("foreign project task leaked")
	}
	if err := tr.Delete(ctx, otherOwner, foreignTask.ID); err != nil {
		t.Fatal(err)
	}

	request("DELETE", "/projects/"+a.ID, "", testToken, 409)
	request("DELETE", "/projects/"+a.ID, "", otherToken, 404)
	request("PATCH", "/tasks/"+item.ID, fmt.Sprintf(`{"version":1,"project_id":%q}`, foreign.ID), testToken, 404)
	request("PATCH", "/tasks/"+item.ID, fmt.Sprintf(`{"version":1,"project_id":%q}`, foreign.ID), otherToken, 404)
	request("PATCH", "/tasks/"+item.ID, fmt.Sprintf(`{"version":1,"project_id":%q}`, b.ID), testToken, 200)
	request("PATCH", "/tasks/"+item.ID, `{"version":1,"project_id":null}`, testToken, 409)
	saved, err := tr.FindByID(ctx, testOwner, item.ID)
	if err != nil || saved.Version != 2 || *saved.ProjectID != b.ID {
		t.Fatal("stale/foreign update changed state")
	}
	request("PATCH", "/tasks/"+item.ID, `{"version":2,"title":"omitted"}`, testToken, 200)
	saved, _ = tr.FindByID(ctx, testOwner, item.ID)
	if saved.ProjectID == nil || *saved.ProjectID != b.ID {
		t.Fatal("omission clears assignment")
	}
	request("DELETE", "/projects/"+a.ID, "", testToken, 204)
	request("DELETE", "/projects/"+b.ID, "", testToken, 409)
	request("PATCH", "/tasks/"+item.ID, `{"version":3,"project_id":null}`, testToken, 200)
	request("DELETE", "/projects/"+b.ID, "", testToken, 204)
	// Bypass the service: PostgreSQL must still reject cross-owner associations.
	saved, _ = tr.FindByID(ctx, testOwner, item.ID)
	saved.ProjectID = &foreign.ID
	if err := tr.Update(ctx, testOwner, saved); !errors.Is(err, domainproject.ErrNotFound) {
		t.Fatalf("FK update: %v", err)
	}
	saved.ID = "cross-owner-insert"
	if err := tr.Save(ctx, testOwner, saved); !errors.Is(err, domainproject.ErrNotFound) {
		t.Fatalf("FK insert: %v", err)
	}
	// Force a deletion between authorization and SQL, covering both INSERT and UPDATE.
	for _, operation := range []string{"create", "update"} {
		p := createProject(testToken)
		reader := projectLookup(func(ctx context.Context, o domainaccount.UserID, id string) (*domainproject.Project, error) {
			found, err := pr.GetByID(ctx, o, id)
			if err != nil {
				return nil, err
			}
			return found, pr.Delete(ctx, o, id)
		})
		raced := applicationtask.New(tr, reader)
		if operation == "create" {
			_, err = raced.Create(ctx, testOwner, applicationtask.CreateInput{Title: "race", Priority: 1, ProjectID: &p.ID})
		} else {
			_, err = raced.Update(ctx, testOwner, item.ID, applicationtask.UpdateInput{Version: versionPointer(4), ProjectID: applicationtask.OptionalString{Present: true, Value: &p.ID}})
		}
		if !errors.Is(err, domainproject.ErrNotFound) {
			t.Fatalf("TOCTOU %s: %v", operation, err)
		}
	}
	saved, _ = tr.FindByID(ctx, testOwner, item.ID)
	if saved.Version != 4 || saved.ProjectID != nil {
		t.Fatal("failed assignment mutated task")
	}
	// Simultaneous deletion/attachment has exactly one winner, enforced by FK locks.
	for i := 0; i < 5; i++ {
		p := createProject(testToken)
		start := make(chan struct{})
		created := make(chan error, 1)
		deleted := make(chan error, 1)
		go func() {
			<-start
			_, err := ts.Create(ctx, testOwner, applicationtask.CreateInput{Title: "race", Priority: 1, ProjectID: &p.ID})
			created <- err
		}()
		go func() { <-start; deleted <- pr.Delete(ctx, testOwner, p.ID) }()
		close(start)
		ce, de := <-created, <-deleted
		if !((ce == nil && errors.Is(de, domainproject.ErrNotEmpty)) || (de == nil && errors.Is(ce, domainproject.ErrNotFound))) {
			t.Fatalf("delete/attach: %v / %v", ce, de)
		}
	}
	// Two moves based on one version: only one can succeed.
	p1, p2 := createProject(testToken), createProject(testToken)
	synced := &synchronizedReadRepository{Repository: tr}
	synced.readers.Add(2)
	concurrent := applicationtask.New(synced, pr)
	outcomes := make(chan error, 2)
	for _, id := range []string{p1.ID, p2.ID} {
		go func(id string) {
			_, err := concurrent.Update(ctx, testOwner, item.ID, applicationtask.UpdateInput{Version: versionPointer(4), ProjectID: applicationtask.OptionalString{Present: true, Value: &id}})
			outcomes <- err
		}(id)
	}
	e1, e2 := <-outcomes, <-outcomes
	if !((e1 == nil && errors.Is(e2, domaintask.ErrTaskConflict)) || (e2 == nil && errors.Is(e1, domaintask.ErrTaskConflict))) {
		t.Fatalf("move CAS: %v / %v", e1, e2)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := pr.List(canceled, testOwner); !errors.Is(err, context.Canceled) {
		t.Fatal("context lost")
	}
	if _, err := pr.List(ctx, ""); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal("owner missing")
	}
	// Closed independent pool simulates an outage without interfering with cleanup.
	dead, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal(err)
	}
	dead.Close()
	offline := gin.New()
	interfaceshttprouter.RegisterProjects(offline, applicationproject.New(infraproject.New(dead), infraproject.New(dead)), auth)
	rec := taskRequest(offline, "GET", "/projects", "", testToken)
	if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"error":{"code":"INTERNAL_ERROR","message":"Internal server error"}}` {
		t.Fatal("database error leaked")
	}
}
