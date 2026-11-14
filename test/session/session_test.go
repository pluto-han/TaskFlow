package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	interfaceshttpaccount "github.com/pluto-han/taskflow/internal/interfaces/http/account"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	interfaceshttpsession "github.com/pluto-han/taskflow/internal/interfaces/http/session"
)

type fakeUsers struct {
	domainaccount.Repository
	u     *domainaccount.User
	err   error
	email string
}

func (f *fakeUsers) GetByEmail(ctx context.Context, email string) (*domainaccount.User, error) {
	f.email = email
	return f.u, f.err
}
func (f *fakeUsers) GetByID(ctx context.Context, id string) (*domainaccount.User, error) {
	return f.u, f.err
}

type fakePasswords struct {
	ok      bool
	err     error
	hashErr error
	seen    string
	calls   int
}

func (f *fakePasswords) HashPassword(context.Context, string) (string, error) {
	return "dummy", f.hashErr
}
func (f *fakePasswords) VerifyPassword(_ context.Context, _ string, hash string) (bool, error) {
	f.seen = hash
	f.calls++
	return f.ok, f.err
}

type fakeSessions struct {
	domainsession.Repository
	p                       domainsession.Principal
	hash                    [32]byte
	err                     error
	creates, finds, revokes int
}

func (f *fakeSessions) Create(_ context.Context, id string, uid domainaccount.UserID, hash [32]byte, _ int64) (domainsession.Principal, error) {
	f.creates++
	f.hash = hash
	f.p = domainsession.Principal{SessionID: id, UserID: uid, ExpiresAt: time.Now().Add(24 * time.Hour)}
	return f.p, f.err
}
func (f *fakeSessions) FindActive(context.Context, [32]byte) (domainsession.Principal, error) {
	f.finds++
	return f.p, f.err
}
func (f *fakeSessions) Revoke(_ context.Context, p domainsession.Principal) error {
	f.revokes++
	f.p = p
	return f.err
}
func fixture(t *testing.T) (*applicationsession.Service, *fakeUsers, *fakeSessions, *fakePasswords) {
	t.Helper()
	u := &fakeUsers{u: &domainaccount.User{ID: "00000000-0000-4000-8000-000000000001", Email: "a@example.com", PasswordHash: "real-hash", CreatedAt: time.Now().UTC()}}
	r := &fakeSessions{p: domainsession.Principal{SessionID: "00000000-0000-4000-8000-000000000002", UserID: domainaccount.UserID(u.u.ID), ExpiresAt: time.Now().Add(time.Hour)}}
	p := &fakePasswords{ok: true}
	s, err := applicationsession.New(context.Background(), u, r, p)
	if err != nil {
		t.Fatal(err)
	}
	return s, u, r, p
}
func TestLoginService(t *testing.T) {
	s, u, r, p := fixture(t)
	input := applicationsession.LoginInput{Email: " A@Example.COM ", Password: "a valid long passphrase"}
	first, err := s.Login(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.AccessToken)
	if err != nil || len(raw) != 32 || first.TokenType != "Bearer" || first.ExpiresAt.IsZero() {
		t.Fatal("invalid login response")
	}
	if r.hash != sha256.Sum256([]byte(first.AccessToken)) || p.seen != "real-hash" || u.email != "a@example.com" {
		t.Fatal("hash/normalization mismatch")
	}
	var id pgtype.UUID
	if err := id.Scan(r.p.SessionID); err != nil || id.Bytes[6]>>4 != 4 {
		t.Fatal("invalid session UUID")
	}
	second, err := s.Login(context.Background(), input)
	if err != nil || second.AccessToken == first.AccessToken {
		t.Fatal("token must be fresh per login")
	}
	p.ok = false
	if _, err := s.Login(context.Background(), input); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal(err)
	}
	u.err = domainaccount.ErrUserNotFound
	p.ok = true
	if _, err := s.Login(context.Background(), input); !errors.Is(err, domainaccount.ErrUnauthorized) || p.seen != "dummy" {
		t.Fatal("missing user must verify dummy and still fail")
	}
	if r.creates != 2 {
		t.Fatal("failed login created session")
	}
}
func TestServiceFailures(t *testing.T) {
	input := applicationsession.LoginInput{Email: "a@example.com", Password: "a valid long passphrase"}
	for _, stage := range []string{"lookup", "verify", "insert"} {
		t.Run(stage, func(t *testing.T) {
			s, u, r, p := fixture(t)
			want := errors.New("private DB or hash details")
			switch stage {
			case "lookup":
				u.err = want
			case "verify":
				p.err = want
			case "insert":
				r.err = want
			}
			result, err := s.Login(context.Background(), input)
			if !errors.Is(err, want) || result.AccessToken != "" {
				t.Fatalf("%v %+v", err, result)
			}
		})
	}
	s, users, r, p := fixture(t)
	for _, bad := range []applicationsession.LoginInput{{}, {Email: "invalid", Password: input.Password}, {Email: input.Email, Password: strings.Repeat("x", 513)}} {
		if _, err := s.Login(context.Background(), bad); !errors.Is(err, domainaccount.ErrUnauthorized) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Login(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if p.calls != 0 || r.creates != 0 {
		t.Fatal("invalid input reached password/session")
	}
	p.hashErr = errors.New("entropy failure")
	if _, err := applicationsession.New(ctx, users, r, p); err == nil {
		t.Fatal("constructor ignored hash failure")
	}
}
func request(router http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func testRouter(s *applicationsession.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	interfaceshttprouter.RegisterSessions(r, s)
	return r
}
func TestHTTPAuthentication(t *testing.T) {
	s, u, r, _ := fixture(t)
	router := testRouter(s)
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, header := range []string{"", "Basic " + token, "Bearer short", "Bearer " + strings.Repeat("!", 43), "Bearer " + token + " extra"} {
		rec := request(router, "GET", "/auth/me", "", header)
		if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("%q: %d", header, rec.Code)
		}
	}
	if r.finds != 0 {
		t.Fatal("malformed tokens reached DB")
	}
	rec := request(router, "GET", "/auth/me?user_id=forged", "", "bearer "+token)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), u.u.ID) || strings.Contains(rec.Body.String(), "hash") {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	req := httptest.NewRequest("GET", "/auth/me", nil)
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Authorization", "Bearer "+token)
	dup := httptest.NewRecorder()
	router.ServeHTTP(dup, req)
	if dup.Code != 401 {
		t.Fatal("accepted duplicate authorization headers")
	}
	rec = request(router, "POST", "/auth/logout", `{"user_id":"forged","session_id":"forged"}`, "Bearer "+token)
	if rec.Code != 204 || r.revokes != 1 || r.p.UserID != domainaccount.UserID(u.u.ID) {
		t.Fatal("logout trusted request identity")
	}
	r.err = domainaccount.ErrUnauthorized
	if rec := request(router, "GET", "/auth/me", "", "Bearer "+token); rec.Code != 401 {
		t.Fatal(rec.Code)
	}
	r.err = errors.New("private SQL")
	rec = request(router, "GET", "/auth/me", "", "Bearer "+token)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "SQL") {
		t.Fatal("repository error leaked")
	}
}
func TestLoginHTTPFailures(t *testing.T) {
	good := `{"email":"a@example.com","password":"a valid long passphrase"}`
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
		setup      func(*fakeUsers, *fakePasswords)
	}{
		{"success", good, 200, "", nil},
		{"bad JSON", "{", 400, "INVALID_REQUEST", nil},
		{"trailing JSON", good + "{}", 400, "INVALID_REQUEST", nil},
		{"oversize", strings.Repeat("x", 4097), 413, "REQUEST_TOO_LARGE", nil},
		{"exact size", good + strings.Repeat(" ", 4096-len(good)), 200, "", nil},
		{"null", "null", 401, "UNAUTHORIZED", nil},
		{"wrong password", good, 401, "UNAUTHORIZED", func(_ *fakeUsers, p *fakePasswords) { p.ok = false }},
		{"missing user", good, 401, "UNAUTHORIZED", func(u *fakeUsers, _ *fakePasswords) { u.err = domainaccount.ErrUserNotFound }},
		{"busy", good, 503, "AUTH_BUSY", func(_ *fakeUsers, p *fakePasswords) { p.err = infrapassword.ErrBusy }},
		{"deadline", good, 504, "REQUEST_TIMEOUT", func(u *fakeUsers, _ *fakePasswords) { u.err = context.DeadlineExceeded }},
		{"db error", good, 500, "INTERNAL_ERROR", func(u *fakeUsers, _ *fakePasswords) { u.err = errors.New("SQL secret") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, u, _, p := fixture(t)
			if tc.setup != nil {
				tc.setup(u, p)
			}
			rec := request(testRouter(s), "POST", "/auth/login", tc.body, "")
			if rec.Code != tc.status || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if tc.code != "" && !strings.Contains(rec.Body.String(), tc.code) {
				t.Fatal(rec.Body.String())
			}
			for _, secret := range []string{"SQL", "secret", "real-hash", "dummy", "a valid long passphrase"} {
				if strings.Contains(rec.Body.String(), secret) {
					t.Fatal("leak")
				}
			}
			if tc.status != 200 && strings.Contains(rec.Body.String(), "access_token") {
				t.Fatal("token on failure")
			}
		})
	}
}
func TestLoginLimiterConcurrentAndReset(t *testing.T) {
	now := time.Now()
	limiter := interfaceshttpsession.NewLoginLimiterWithClock(func() time.Time { return now })
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiterAllows(limiter) {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 30 {
		t.Fatal(passed.Load())
	}
	now = now.Add(time.Minute)
	if !limiterAllows(limiter) {
		t.Fatal("window did not reset")
	}
	s, _, _, _ := fixture(t)
	router := testRouter(s)
	for i := 0; i < 30; i++ {
		request(router, "POST", "/auth/login", "{}", "")
	}
	rec := request(router, "POST", "/auth/login", "{}", "")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatal(rec.Code)
	}
}
func TestPrincipalContextAndRepositoryMapping(t *testing.T) {
	if _, ok := interfaceshttpmiddleware.CurrentPrincipal(context.Background()); ok {
		t.Fatal("anonymous principal")
	}
	s, u, _, _ := fixture(t)
	u.err = domainaccount.ErrUserNotFound
	if _, err := s.Me(context.Background(), domainsession.Principal{}); !errors.Is(err, domainaccount.ErrUnauthorized) {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(interfaceshttpaccount.UserResponse{})
	if strings.Contains(string(raw), "password") {
		t.Fatal("DTO contains password")
	}
}

func sessionWithRepository(t *testing.T, users *fakeUsers, repo domainsession.Repository, passwords *fakePasswords) *applicationsession.Service {
	t.Helper()
	service, err := applicationsession.New(context.Background(), users, repo, passwords)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func limiterAllows(limiter *interfaceshttpsession.LoginLimiter) bool {
	router := gin.New()
	router.GET("/", limiter.Middleware, func(c *gin.Context) { c.Status(204) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	return response.Code == 204
}
