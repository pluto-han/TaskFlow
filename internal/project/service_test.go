package project

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pluto-han/taskflow/internal/session"
)

type captureRepository struct {
	Repository
	saved *Project
}

func (r *captureRepository) Create(_ context.Context, owner session.UserID, p *Project) (*Project, error) {
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
		s := NewService(r)
		p, err := s.Create(context.Background(), "owner", CreateRequest{Name: tc.name, Description: tc.description})
		if tc.valid {
			if err != nil || p.Name != strings.TrimSpace(tc.name) {
				t.Fatalf("valid: %v", err)
			}
			if _, err := ParseID(p.ID); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, ErrInvalid) || r.saved != nil {
			t.Fatalf("invalid input saved: %v", err)
		}
	}
}
func TestInvalidRequestsStopBeforeRepository(t *testing.T) {
	s := NewService(nil)
	ctx := context.Background()
	id := "00000000-0000-4000-8000-000000000001"
	if _, err := s.Create(ctx, "", CreateRequest{Name: "valid"}); !errors.Is(err, session.ErrUnauthorized) {
		t.Fatal(err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.List(canceled, "owner"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.GetByID(ctx, "owner", "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "owner", "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	blank := " "
	long := strings.Repeat("x", 2001)
	for _, input := range []UpdateRequest{{}, {Name: &blank}, {Description: &long}} {
		if _, err := s.Update(ctx, "owner", id, input); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}
