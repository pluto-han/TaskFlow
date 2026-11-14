package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	interfaceshttptask "github.com/pluto-han/taskflow/internal/interfaces/http/task"
	"github.com/pluto-han/taskflow/test/support/task"
)

type projectLookup func(context.Context, domainaccount.UserID, string) (*domainproject.Project, error)

func (f projectLookup) GetByID(ctx context.Context, o domainaccount.UserID, id string) (*domainproject.Project, error) {
	return f(ctx, o, id)
}

func TestTaskProjectAssignment(t *testing.T) {
	ctx := context.Background()
	id := "00000000-0000-4000-8000-000000000003"
	lookup := projectLookup(func(_ context.Context, o domainaccount.UserID, p string) (*domainproject.Project, error) {
		if o == testOwner && p == id {
			return &domainproject.Project{ID: id, OwnerID: o}, nil
		}
		return nil, domainproject.ErrNotFound
	})
	repo := task.NewMemoryRepository()
	s := applicationtask.New(repo, lookup)
	for _, tc := range []struct {
		id    string
		owner domainaccount.UserID
		want  error
	}{{"bad", testOwner, domainproject.ErrInvalid}, {id, otherOwner, domainproject.ErrNotFound}, {"00000000-0000-4000-8000-000000000004", testOwner, domainproject.ErrNotFound}} {
		if _, err := s.Create(ctx, tc.owner, applicationtask.CreateInput{Title: "task", Priority: 1, ProjectID: &tc.id}); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	item, err := s.Create(ctx, testOwner, applicationtask.CreateInput{Title: "task", Priority: 1, ProjectID: &id})
	if err != nil {
		t.Fatal(err)
	}
	title := "renamed"
	item, err = s.Update(ctx, testOwner, item.ID, applicationtask.UpdateInput{Title: &title, Version: versionPointer(1)})
	if err != nil || item.ProjectID == nil || *item.ProjectID != id || item.Version != 2 {
		t.Fatalf("omission: %v", err)
	}
	*item.ProjectID = "mutated"
	saved, _ := repo.FindByID(ctx, testOwner, item.ID)
	if *saved.ProjectID != id {
		t.Fatal("project pointer aliased")
	}
	if _, err = s.Update(ctx, testOwner, item.ID, applicationtask.UpdateInput{ProjectID: applicationtask.OptionalString{Present: true}, Version: versionPointer(1)}); !errors.Is(err, domaintask.ErrTaskConflict) {
		t.Fatal(err)
	}
	item, err = s.Update(ctx, testOwner, item.ID, applicationtask.UpdateInput{ProjectID: applicationtask.OptionalString{Present: true}, Version: versionPointer(2)})
	if err != nil || item.ProjectID != nil || item.Version != 3 {
		t.Fatalf("clear: %v", err)
	}
}
func TestOptionalProjectJSON(t *testing.T) {
	for _, tc := range []struct {
		body           string
		present, valid bool
	}{{`{}`, false, true}, {`{"project_id":null}`, true, true}, {`{"project_id":"id"}`, true, true}, {`{"project_id":12}`, true, false}, {`{"project_id":{}}`, true, false}} {
		var input interfaceshttptask.UpdateTaskRequest
		err := json.Unmarshal([]byte(tc.body), &input)
		if (err == nil) != tc.valid || (tc.valid && input.ProjectID.Present != tc.present) {
			t.Fatalf("%s: %v", tc.body, err)
		}
	}
}

func TestProjectListFilter(t *testing.T) {
	ctx := context.Background()
	repo := task.NewMemoryRepository()
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, item := range []*domaintask.Task{{ID: "1", ProjectID: &id, Title: "one", Priority: 1}, {ID: "2", ProjectID: &id, Title: "two", Priority: 1}, {ID: "3", Title: "none", Priority: 1}} {
		item.CreatedAt = time.Now().UTC()
		if err := repo.Save(ctx, testOwner, item); err != nil {
			t.Fatal(err)
		}
	}
	service := applicationtask.New(repo, nil)
	options, err := interfaceshttptask.ParseListOptions("project_id=AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA&limit=1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.List(ctx, testOwner, options)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first page: %v", err)
	}
	options.Cursor = page.NextCursor
	page, err = service.List(ctx, testOwner, options)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "" {
		t.Fatalf("second page: %v", err)
	}
	none := "none"
	options.ProjectID = &none
	if _, err := service.List(ctx, testOwner, options); !errors.Is(err, domaintask.ErrInvalidList) {
		t.Fatal("cross-filter cursor accepted")
	}
	options.Cursor = ""
	page, err = service.List(ctx, testOwner, options)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "3" {
		t.Fatal("unclassified filter")
	}
	for _, q := range []string{"project_id=", "project_id=bad", "project_id=none&project_id=none"} {
		if _, err := interfaceshttptask.ParseListOptions(q); !errors.Is(err, domaintask.ErrInvalidList) {
			t.Fatalf("accepted %s", q)
		}
	}
	page, err = service.List(ctx, otherOwner, options)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("owner isolation lost")
	}
}
