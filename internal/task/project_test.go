package task

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
