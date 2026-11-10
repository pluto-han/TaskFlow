package session

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/auth"
)

type managementSessions struct {
	Repository
	rows             []SessionInfo
	err              error
	changed, revoked int
	version          int64
	hash, target     string
	actor            Principal
}

func (r *managementSessions) ListActive(_ context.Context, _ UserID, _ ListQuery) ([]SessionInfo, error) {
	return r.rows, r.err
}
func (r *managementSessions) RevokeOwned(_ context.Context, p Principal, id string) error {
	r.revoked++
	r.actor = p
	r.target = id
	return r.err
}
func (r *managementSessions) ChangePassword(_ context.Context, p Principal, version int64, hash string) error {
	r.changed++
	r.actor = p
	r.version = version
	r.hash = hash
	return r.err
}

func TestPasswordChangeService(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*fakeUsers, *fakePasswords, *managementSessions)
		want  error
	}{
		{"success", nil, nil},
		{"wrong current", func(_ *fakeUsers, p *fakePasswords, _ *managementSessions) { p.ok = false }, ErrCurrentPassword},
		{"verify busy", func(_ *fakeUsers, p *fakePasswords, _ *managementSessions) { p.err = auth.ErrBusy }, auth.ErrBusy},
		{"hash busy", func(_ *fakeUsers, p *fakePasswords, _ *managementSessions) { p.hashErr = auth.ErrBusy }, auth.ErrBusy},
		{"changed concurrently", func(_ *fakeUsers, _ *fakePasswords, r *managementSessions) { r.err = ErrCredentialsChanged }, ErrCredentialsChanged},
		{"session revoked", func(_ *fakeUsers, _ *fakePasswords, r *managementSessions) { r.err = ErrUnauthorized }, ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, u, base, passwords := fixture(t)
			u.u.CredentialsVersion = 7
			r := &managementSessions{Repository: base}
			s.sessions = r
			if tc.setup != nil {
				tc.setup(u, passwords, r)
			}
			err := s.ChangePassword(context.Background(), base.p, PasswordChangeRequest{CurrentPassword: "old password", NewPassword: "new password"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want == nil && (r.changed != 1 || r.version != 7 || r.hash == "new password" || r.actor != base.p) {
				t.Fatal("wrong credential snapshot or plaintext password")
			}
			if (tc.name == "wrong current" || tc.name == "verify busy" || tc.name == "hash busy") && r.changed != 0 {
				t.Fatal("failed password verification reached transaction")
			}
		})
	}
}

func TestPasswordChangeValidation(t *testing.T) {
	s, _, base, passwords := fixture(t)
	r := &managementSessions{Repository: base}
	s.sessions = r
	for _, input := range []PasswordChangeRequest{
		{}, {CurrentPassword: "old", NewPassword: "1234567"}, {CurrentPassword: "old", NewPassword: strings.Repeat("a", 129)},
		{CurrentPassword: "old", NewPassword: strings.Repeat(" ", 8)}, {CurrentPassword: strings.Repeat("a", 513), NewPassword: "new password"},
		{CurrentPassword: "old", NewPassword: "invalid\xffpassword"},
	} {
		if err := s.ChangePassword(context.Background(), base.p, input); !errors.Is(err, ErrInvalidPasswordChange) {
			t.Fatal("accepted invalid password change")
		}
	}
	if passwords.calls != 0 || r.changed != 0 {
		t.Fatal("invalid input reached hashing or transaction")
	}
	if err := s.ChangePassword(context.Background(), base.p, PasswordChangeRequest{CurrentPassword: "old", NewPassword: "12345678"}); err != nil {
		t.Fatal("8-character password rejected")
	}
	if err := s.ChangePassword(context.Background(), base.p, PasswordChangeRequest{CurrentPassword: "old", NewPassword: strings.Repeat("界", 128)}); err != nil {
		t.Fatal("valid Unicode password rejected")
	}
}

func TestSessionListAndCursorScope(t *testing.T) {
	s, _, base, _ := fixture(t)
	r := &managementSessions{Repository: base}
	s.sessions = r
	now := time.Now().UTC()
	r.rows = []SessionInfo{{ID: base.p.SessionID, CreatedAt: now}, {ID: "00000000-0000-4000-8000-000000000003", CreatedAt: now.Add(-time.Second)}}
	page, err := s.ListSessions(context.Background(), base.p, "limit=1")
	if err != nil || len(page.Items) != 1 || !page.Items[0].Current || page.NextCursor == "" {
		t.Fatal("invalid bounded session page")
	}
	parsed, err := parseSessions("cursor="+url.QueryEscape(page.NextCursor), base.p.UserID)
	if err != nil || parsed.After.ID != base.p.SessionID {
		t.Fatal("cursor does not round-trip")
	}
	if _, err := parseSessions("cursor="+page.NextCursor, UserID("00000000-0000-4000-8000-000000000099")); !errors.Is(err, ErrInvalidSessionQuery) {
		t.Fatal("accepted another user's cursor")
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=", "limit=1&limit=2", "cursor=", "cursor=bad!", "user_id=forged", "cursor=" + base64.RawURLEncoding.EncodeToString([]byte(`null`))} {
		if _, err := parseSessions(query, base.p.UserID); !errors.Is(err, ErrInvalidSessionQuery) {
			t.Fatalf("accepted %s", query)
		}
	}
	r.rows = nil
	empty, err := s.ListSessions(context.Background(), base.p, "")
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal("empty items must serialize as an array")
	}
}

func TestSessionManagementHTTP(t *testing.T) {
	s, _, base, _ := fixture(t)
	r := &managementSessions{Repository: base}
	s.sessions = r
	router := testRouter(s)
	token := "Bearer " + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/auth/sessions", "", 200},
		{"GET", "/auth/sessions?limit=101", "", 400},
		{"DELETE", "/auth/sessions/invalid", "", 400},
		{"DELETE", "/auth/sessions/00000000-0000-4000-8000-000000000003", "", 204},
		{"POST", "/auth/password/change", `{"current_password":"old password","new_password":"new password"}`, 204},
		{"POST", "/auth/password/change", `{"current_password":"old","new_password":"1234567"}`, 400},
		{"POST", "/auth/password/change", `{"current_password":"old","new_password":"new password","user_id":"forged"}`, 400},
		{"POST", "/auth/password/change", `{} {}`, 400},
		{"POST", "/auth/password/change", strings.Repeat("x", 4097), 413},
	} {
		rec := request(router, tc.method, tc.path, tc.body, token)
		if rec.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("management response is cacheable")
		}
	}
	if r.actor.UserID != base.p.UserID || r.target != "00000000-0000-4000-8000-000000000003" {
		t.Fatal("request identity was not server-derived")
	}
	r.err = ErrSessionNotFound
	if rec := request(router, "DELETE", "/auth/sessions/00000000-0000-4000-8000-000000000003", "", token); rec.Code != 404 {
		t.Fatal("missing session must not reveal ownership")
	}
}
