package task

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/session"
)

type projectLookup func(context.Context, session.UserID, string) (*project.Project, error)

func (f projectLookup) GetByID(ctx context.Context, o session.UserID, id string) (*project.Project, error) {
	return f(ctx, o, id)
}

func TestTaskProjectAssignment(t *testing.T) {
	ctx := context.Background()
	id := "00000000-0000-4000-8000-000000000003"
	lookup := projectLookup(func(_ context.Context, o session.UserID, p string) (*project.Project, error) {
		if o == testOwner && p == id {
			return &project.Project{ID: id, OwnerID: o}, nil
		}
		return nil, project.ErrNotFound
	})
	repo := NewMemoryRepository()
	s := NewService(repo, lookup)
	for _, tc := range []struct {
		id    string
		owner session.UserID
		want  error
	}{{"bad", testOwner, project.ErrInvalid}, {id, otherOwner, project.ErrNotFound}, {"00000000-0000-4000-8000-000000000004", testOwner, project.ErrNotFound}} {
		if _, err := s.Create(ctx, tc.owner, CreateTaskRequest{Title: "task", Priority: 1, ProjectID: &tc.id}); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	item, err := s.Create(ctx, testOwner, CreateTaskRequest{Title: "task", Priority: 1, ProjectID: &id})
	if err != nil {
		t.Fatal(err)
	}
	title := "renamed"
	item, err = s.Update(ctx, testOwner, item.ID, UpdateTaskRequest{Title: &title, Version: versionPointer(1)})
	if err != nil || item.ProjectID == nil || *item.ProjectID != id || item.Version != 2 {
		t.Fatalf("omission: %v", err)
	}
	*item.ProjectID = "mutated"
	saved, _ := repo.FindByID(ctx, testOwner, item.ID)
	if *saved.ProjectID != id {
		t.Fatal("project pointer aliased")
	}
	if _, err = s.Update(ctx, testOwner, item.ID, UpdateTaskRequest{ProjectID: OptionalString{Present: true}, Version: versionPointer(1)}); !errors.Is(err, ErrTaskConflict) {
		t.Fatal(err)
	}
	item, err = s.Update(ctx, testOwner, item.ID, UpdateTaskRequest{ProjectID: OptionalString{Present: true}, Version: versionPointer(2)})
	if err != nil || item.ProjectID != nil || item.Version != 3 {
		t.Fatalf("clear: %v", err)
	}
}
func TestOptionalProjectJSON(t *testing.T) {
	for _, tc := range []struct {
		body           string
		present, valid bool
	}{{`{}`, false, true}, {`{"project_id":null}`, true, true}, {`{"project_id":"id"}`, true, true}, {`{"project_id":12}`, true, false}, {`{"project_id":{}}`, true, false}} {
		var input UpdateTaskRequest
		err := json.Unmarshal([]byte(tc.body), &input)
		if (err == nil) != tc.valid || (tc.valid && input.ProjectID.Present != tc.present) {
			t.Fatalf("%s: %v", tc.body, err)
		}
	}
}

func TestProjectListFilter(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, item := range []*Task{{ID: "1", ProjectID: &id, Title: "one", Priority: 1}, {ID: "2", ProjectID: &id, Title: "two", Priority: 1}, {ID: "3", Title: "none", Priority: 1}} {
		item.CreatedAt = time.Now().UTC()
		if err := repo.Save(ctx, testOwner, item); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(repo, nil)
	options, err := ParseListOptions("project_id=AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA&limit=1")
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
	if _, err := service.List(ctx, testOwner, options); !errors.Is(err, ErrInvalidList) {
		t.Fatal("cross-filter cursor accepted")
	}
	options.Cursor = ""
	page, err = service.List(ctx, testOwner, options)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "3" {
		t.Fatal("unclassified filter")
	}
	for _, q := range []string{"project_id=", "project_id=bad", "project_id=none&project_id=none"} {
		if _, err := ParseListOptions(q); !errors.Is(err, ErrInvalidList) {
			t.Fatalf("accepted %s", q)
		}
	}
	page, err = service.List(ctx, otherOwner, options)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("owner isolation lost")
	}
}
