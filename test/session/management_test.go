package session_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
)

type managementSessions struct {
	domainsession.Repository
	rows             []domainsession.SessionInfo
	query            domainsession.ListQuery
	err              error
	changed, revoked int
	version          int64
	hash, target     string
	actor            domainsession.Principal
}

func (r *managementSessions) ListActive(_ context.Context, _ domainaccount.UserID, q domainsession.ListQuery) ([]domainsession.SessionInfo, error) {
	r.query = q
	return r.rows, r.err
}
func (r *managementSessions) RevokeOwned(_ context.Context, p domainsession.Principal, id string) error {
	r.revoked++
	r.actor = p
	r.target = id
	return r.err
}
func (r *managementSessions) ChangePassword(_ context.Context, p domainsession.Principal, version int64, hash string) error {
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
		{"wrong current", func(_ *fakeUsers, p *fakePasswords, _ *managementSessions) { p.ok = false }, domainsession.ErrCurrentPassword},
		{"verify busy", func(_ *fakeUsers, p *fakePasswords, _ *managementSessions) { p.err = infrapassword.ErrBusy }, infrapassword.ErrBusy},
		{"hash busy", func(_ *fakeUsers, p *fakePasswords, _ *managementSessions) { p.hashErr = infrapassword.ErrBusy }, infrapassword.ErrBusy},
		{"changed concurrently", func(_ *fakeUsers, _ *fakePasswords, r *managementSessions) {
			r.err = domainsession.ErrCredentialsChanged
		}, domainsession.ErrCredentialsChanged},
		{"session revoked", func(_ *fakeUsers, _ *fakePasswords, r *managementSessions) { r.err = domainaccount.ErrUnauthorized }, domainaccount.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, u, base, passwords := fixture(t)
			u.u.CredentialsVersion = 7
			r := &managementSessions{Repository: base}
			s := sessionWithRepository(t, u, r, passwords)
			if tc.setup != nil {
				tc.setup(u, passwords, r)
			}
			err := s.ChangePassword(context.Background(), base.p, applicationsession.PasswordChangeInput{CurrentPassword: "old password", NewPassword: "new password"})
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
	_, u, base, passwords := fixture(t)
	r := &managementSessions{Repository: base}
	s := sessionWithRepository(t, u, r, passwords)
	for _, input := range []applicationsession.PasswordChangeInput{
		{}, {CurrentPassword: "old", NewPassword: "1234567"}, {CurrentPassword: "old", NewPassword: strings.Repeat("a", 129)},
		{CurrentPassword: "old", NewPassword: strings.Repeat(" ", 8)}, {CurrentPassword: strings.Repeat("a", 513), NewPassword: "new password"},
		{CurrentPassword: "old", NewPassword: "invalid\xffpassword"},
	} {
		if err := s.ChangePassword(context.Background(), base.p, input); !errors.Is(err, domainsession.ErrInvalidPasswordChange) {
			t.Fatal("accepted invalid password change")
		}
	}
	if passwords.calls != 0 || r.changed != 0 {
		t.Fatal("invalid input reached hashing or transaction")
	}
	if err := s.ChangePassword(context.Background(), base.p, applicationsession.PasswordChangeInput{CurrentPassword: "old", NewPassword: "12345678"}); err != nil {
		t.Fatal("8-character password rejected")
	}
	if err := s.ChangePassword(context.Background(), base.p, applicationsession.PasswordChangeInput{CurrentPassword: "old", NewPassword: strings.Repeat("界", 128)}); err != nil {
		t.Fatal("valid Unicode password rejected")
	}
}

func TestSessionListAndCursorScope(t *testing.T) {
	_, u, base, passwords := fixture(t)
	r := &managementSessions{Repository: base}
	s := sessionWithRepository(t, u, r, passwords)
	now := time.Now().UTC()
	r.rows = []domainsession.SessionInfo{{ID: base.p.SessionID, CreatedAt: now}, {ID: "00000000-0000-4000-8000-000000000003", CreatedAt: now.Add(-time.Second)}}
	page, err := s.ListSessions(context.Background(), base.p, applicationsession.ListInput{Limit: 1})
	if err != nil || len(page.Items) != 1 || !page.Items[0].Current || page.NextCursor == "" {
		t.Fatal("invalid bounded session page")
	}
	_, err = s.ListSessions(context.Background(), base.p, applicationsession.ListInput{Cursor: page.NextCursor})
	parsed := r.query
	if err != nil || parsed.After.ID != base.p.SessionID {
		t.Fatal("cursor does not round-trip")
	}
	if _, err := s.ListSessions(context.Background(), domainsession.Principal{UserID: domainaccount.UserID("00000000-0000-4000-8000-000000000099")}, applicationsession.ListInput{Cursor: page.NextCursor}); !errors.Is(err, domainsession.ErrInvalidSessionQuery) {
		t.Fatal("accepted another user's cursor")
	}
	router := testRouter(s)
	token := "Bearer " + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, query := range []string{"limit=0", "limit=101", "limit=", "limit=1&limit=2", "cursor=", "cursor=bad!", "user_id=forged", "cursor=" + base64.RawURLEncoding.EncodeToString([]byte(`null`))} {
		if rec := request(router, "GET", "/auth/sessions?"+query, "", token); rec.Code != 400 {
			t.Fatalf("accepted %s", query)
		}
	}
	r.rows = nil
	empty, err := s.ListSessions(context.Background(), base.p, applicationsession.ListInput{})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal("empty items must serialize as an array")
	}
}

func TestSessionManagementHTTP(t *testing.T) {
	_, u, base, passwords := fixture(t)
	r := &managementSessions{Repository: base}
	s := sessionWithRepository(t, u, r, passwords)
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
	r.err = domainsession.ErrSessionNotFound
	if rec := request(router, "DELETE", "/auth/sessions/00000000-0000-4000-8000-000000000003", "", token); rec.Code != 404 {
		t.Fatal("missing session must not reveal ownership")
	}
}
