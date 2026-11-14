package project_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

type captureRepository struct {
	domainproject.Repository
	saved *domainproject.Project
}

func (r *captureRepository) Create(_ context.Context, owner domainaccount.UserID, p *domainproject.Project) (*domainproject.Project, error) {
	r.saved = p
	return p, nil
}

func TestCreateValidation(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		valid             bool
	}{
		{"  项目  ", "", true}, {strings.Repeat("项", 100), strings.Repeat("述", 2000), true},
		{" ", "", false}, {strings.Repeat("项", 101), "", false}, {"ok", strings.Repeat("述", 2001), false}, {"a\x00", "", false}, {"ok", "\x00", false},
	} {
		r := &captureRepository{}
		s := applicationproject.New(r, nil)
		p, err := s.Create(context.Background(), "owner", applicationproject.CreateInput{Name: tc.name, Description: tc.description})
		if tc.valid {
			if err != nil || p.Name != strings.TrimSpace(tc.name) {
				t.Fatalf("valid: %v", err)
			}
			if _, err := domainproject.ParseID(p.ID); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, domainproject.ErrInvalid) || r.saved != nil {
			t.Fatalf("invalid input saved: %v", err)
		}
	}
}
func TestInvalidRequestsStopBeforeRepository(t *testing.T) {
	s := applicationproject.New(nil, nil)
	ctx := context.Background()
	id := "00000000-0000-4000-8000-000000000001"
	if _, err := s.Create(ctx, "", applicationproject.CreateInput{Name: "valid"}); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal(err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.List(canceled, "owner"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.GetByID(ctx, "owner", "bad"); !errors.Is(err, domainproject.ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "owner", "bad"); !errors.Is(err, domainproject.ErrInvalid) {
		t.Fatal(err)
	}
	blank := " "
	long := strings.Repeat("x", 2001)
	for _, input := range []domainproject.Change{{}, {Name: &blank}, {Description: &long}} {
		if _, err := s.Update(ctx, "owner", id, input); !errors.Is(err, domainproject.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
