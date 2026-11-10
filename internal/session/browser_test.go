package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type browserSessions struct {
	Repository
	rows                    map[[32]byte]BrowserPrincipal
	err                     error
	creates, finds, revokes int
}

func (r *browserSessions) CreateBrowser(_ context.Context, id string, uid UserID, hash, csrf [32]byte, _ int64) (Principal, error) {
	r.creates++
	if r.err != nil {
		return Principal{}, r.err
	}
	p := Principal{SessionID: id, UserID: uid, ExpiresAt: time.Now().Add(24 * time.Hour)}
	r.rows[hash] = BrowserPrincipal{Principal: p, CSRFSecret: csrf}
	return p, nil
}
func (r *browserSessions) FindBrowser(_ context.Context, hash [32]byte) (BrowserPrincipal, error) {
	r.finds++
	if r.err != nil {
		return BrowserPrincipal{}, r.err
	}
	p, ok := r.rows[hash]
	if !ok || !p.ExpiresAt.After(time.Now()) {
		return BrowserPrincipal{}, ErrUnauthorized
	}
	return p, nil
}
func (r *browserSessions) Revoke(_ context.Context, p Principal) error {
	r.revokes++
	if r.err != nil {
		return r.err
	}
	for hash, row := range r.rows {
		if row.SessionID == p.SessionID && row.UserID == p.UserID {
			delete(r.rows, hash)
			return nil
		}
	}
	return ErrUnauthorized
}

func browserFixture(t *testing.T, origin string) (*gin.Engine, *browserSessions, *fakePasswords) {
	t.Helper()
	s, _, legacy, passwords := fixture(t)
	r := &browserSessions{Repository: legacy, rows: make(map[[32]byte]BrowserPrincipal)}
	s.sessions = r
	router := gin.New()
	h := Mount(router, s, httpapi.SecurityOptions{PublicOrigin: origin, Production: strings.HasPrefix(origin, "https://")})
	protected := func(c *gin.Context) {
		p, _ := CurrentPrincipal(c.Request.Context())
		c.JSON(200, gin.H{"user_id": p.UserID})
	}
	router.GET("/protected", h.RequireSession, protected)
	router.POST("/protected", h.RequireSession, protected)
	return router, r, passwords
}

func browserRequest(router http.Handler, method, path, body string, cookie *http.Cookie, origin, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func loginBrowser(t *testing.T, router http.Handler, origin string) (*http.Cookie, BrowserResponse) {
	t.Helper()
	rec := browserRequest(router, "POST", "/auth/browser/login", `{"email":"a@example.com","password":"long test password"}`, nil, origin, "")
	if rec.Code != 200 {
		t.Fatalf("browser login: %d", rec.Code)
	}
	var response BrowserResponse
	if json.Unmarshal(rec.Body.Bytes(), &response) != nil || len(rec.Result().Cookies()) != 1 {
		t.Fatal("invalid browser login shape")
	}
	cookie := rec.Result().Cookies()[0]
	if strings.Contains(rec.Body.String(), cookie.Value) || strings.Contains(rec.Body.String(), "access_token") || strings.Contains(rec.Body.String(), "password") {
		t.Fatal("login credential leaked in JSON")
	}
	return cookie, response
}

func TestBrowserSessionLifecycle(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:5173", "https://tasks.example.com"} {
		t.Run(origin, func(t *testing.T) {
			router, r, _ := browserFixture(t, origin)
			cookie, response := loginBrowser(t, router, origin)
			wantName := developmentCookieName
			secure := strings.HasPrefix(origin, "https://")
			if secure {
				wantName = secureCookieName
			}
			if cookie.Name != wantName || cookie.Secure != secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge < 86390 || cookie.MaxAge > 86400 || cookie.Expires.After(response.ExpiresAt) {
				t.Fatal("unsafe cookie attributes")
			}
			digest := sha256.Sum256([]byte(cookie.Value))
			csrf, _ := base64.RawURLEncoding.Strict().DecodeString(response.CSRFToken)
			stored := r.rows[digest]
			if len(csrf) != 32 || !bytes.Equal(csrf, stored.CSRFSecret[:]) || response.User.ID != string(stored.UserID) {
				t.Fatal("browser session mismatch")
			}
			for i := 0; i < 2; i++ {
				rec := browserRequest(router, "GET", "/auth/browser/session", "", cookie, "", "")
				var restored BrowserResponse
				if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &restored) != nil || restored.CSRFToken != response.CSRFToken || !restored.ExpiresAt.Equal(response.ExpiresAt) || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
					t.Fatal("bootstrap rotated/renewed the session")
				}
			}
			if rec := browserRequest(router, "POST", "/protected", `{"user_id":"forged"}`, cookie, origin, response.CSRFToken); rec.Code != 200 || !strings.Contains(rec.Body.String(), response.User.ID) {
				t.Fatal("cookie write failed")
			}
			r.err = errors.New("private database details")
			rec := browserRequest(router, "GET", "/auth/browser/session", "", cookie, "", "")
			if rec.Code != 500 || rec.Header().Get("Set-Cookie") != "" || strings.Contains(rec.Body.String(), "private") {
				t.Fatal("database failure cleared cookie or leaked details")
			}
			r.err = nil
			rec = browserRequest(router, "POST", "/auth/browser/logout", "", cookie, origin, response.CSRFToken)
			removed := rec.Result().Cookies()
			if rec.Code != 204 || r.revokes != 1 || len(removed) != 1 || removed[0].Name != cookie.Name || removed[0].Path != "/" || removed[0].MaxAge != -1 || !removed[0].HttpOnly || removed[0].Secure != secure {
				t.Fatal("logout did not revoke and clear")
			}
			if rec := browserRequest(router, "GET", "/auth/browser/session", "", cookie, "", ""); rec.Code != 401 || len(rec.Result().Cookies()) != 1 {
				t.Fatal("revoked session restored")
			}
		})
	}
}

func TestBrowserCSRFAndOrigins(t *testing.T) {
	origin := "https://tasks.example.com"
	router, r, _ := browserFixture(t, origin)
	cookie, response := loginBrowser(t, router, origin)
	for _, tc := range []struct {
		name, origin, csrf             string
		duplicateOrigin, duplicateCSRF bool
		fetchSite                      string
		status                         int
	}{
		{"missing origin", "", response.CSRFToken, false, false, "", 403},
		{"foreign origin", "https://evil.example", response.CSRFToken, false, false, "", 403},
		{"same-site subdomain", "https://evil.example.com", response.CSRFToken, false, false, "same-site", 403},
		{"null origin", "null", response.CSRFToken, false, false, "", 403},
		{"duplicate origin", origin, response.CSRFToken, true, false, "", 403},
		{"missing csrf", origin, "", false, false, "", 403},
		{"wrong csrf", origin, base64.RawURLEncoding.EncodeToString(make([]byte, 32)), false, false, "", 403},
		{"short csrf", origin, "short", false, false, "", 403},
		{"malformed csrf", origin, strings.Repeat("!", 43), false, false, "", 403},
		{"duplicate csrf", origin, response.CSRFToken, false, true, "", 403},
		{"explicit cross site", origin, response.CSRFToken, false, false, "cross-site", 403},
		{"valid", origin, response.CSRFToken, false, false, "same-origin", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/auth/browser/logout", nil)
			req.AddCookie(cookie)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.csrf != "" {
				req.Header.Set("X-CSRF-Token", tc.csrf)
			}
			if tc.duplicateOrigin {
				req.Header.Add("Origin", tc.origin)
			}
			if tc.duplicateCSRF {
				req.Header.Add("X-CSRF-Token", tc.csrf)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("got %d want %d", rec.Code, tc.status)
			}
			if tc.status == 403 && (r.revokes != 0 || rec.Header().Get("Set-Cookie") != "") {
				t.Fatal("rejected request changed session")
			}
		})
	}
}

func TestBrowserCredentialSelection(t *testing.T) {
	origin := "http://127.0.0.1:5173"
	router, _, _ := browserFixture(t, origin)
	cookie, _ := loginBrowser(t, router, origin)
	for _, tc := range []struct {
		name, rawCookie, authorization string
		status                         int
	}{
		{"absent", "", "", 401},
		{"valid cookie", cookie.String(), "", 200},
		{"empty cookie", cookie.Name + "=", "", 401},
		{"wrong environment", secureCookieName + "=" + cookie.Value, "", 401},
		{"duplicate cookie", cookie.Name + "=" + cookie.Value + "; " + cookie.Name + "=" + cookie.Value, "", 401},
		{"mixed environments", cookie.Name + "=" + cookie.Value + "; " + secureCookieName + "=" + cookie.Value, "", 401},
		{"both", cookie.Name + "=" + cookie.Value, "Bearer other", 400},
		{"empty plus bearer", cookie.Name + "=", "Bearer other", 400},
		{"malformed plus bearer", cookie.Name + "=\"invalid", "Bearer other", 400},
		{"legacy", "unrelated=value", "Bearer " + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/protected", nil)
			if tc.rawCookie != "" {
				req.Header.Set("Cookie", tc.rawCookie)
			}
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("got %d want %d", rec.Code, tc.status)
			}
		})
	}
	if rec := browserRequest(router, "GET", "/auth/me", "", cookie, "", ""); rec.Code != 401 {
		t.Fatal("legacy route accepted a browser cookie")
	}
	req := httptest.NewRequest("GET", "/auth/browser/session", nil)
	req.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("browser endpoint accepted a Bearer-only request")
	}
}

func TestBrowserLoginRejectionAndFreshness(t *testing.T) {
	origin := "http://127.0.0.1:5173"
	router, r, passwords := browserFixture(t, origin)
	body := `{"email":"a@example.com","password":"long test password"}`
	for _, tc := range []struct {
		origin, body string
		status       int
	}{
		{"", body, 403}, {"null", body, 403}, {"https://evil.example", body, 403},
		{origin, "{", 400}, {origin, strings.Repeat("x", 4097), 413},
	} {
		if rec := browserRequest(router, "POST", "/auth/browser/login", tc.body, nil, tc.origin, ""); rec.Code != tc.status || rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("login rejection failed")
		}
	}
	if r.creates != 0 || passwords.calls != 0 {
		t.Fatal("rejected login reached credentials")
	}
	passwords.ok = false
	if rec := browserRequest(router, "POST", "/auth/browser/login", body, nil, origin, ""); rec.Code != 401 || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("wrong password accepted")
	}
	passwords.ok = true
	first, a := loginBrowser(t, router, origin)
	second, b := loginBrowser(t, router, origin)
	if first.Value == second.Value || a.CSRFToken == b.CSRFToken || r.creates != 2 {
		t.Fatal("login reused credentials")
	}
	hash := sha256.Sum256([]byte(second.Value))
	expired := r.rows[hash]
	expired.ExpiresAt = time.Now().Add(-time.Second)
	r.rows[hash] = expired
	if rec := browserRequest(router, "GET", "/auth/browser/session", "", second, "", ""); rec.Code != 401 {
		t.Fatal("expired session restored")
	}
}

func TestLoginPathsShareRouteLocalBudget(t *testing.T) {
	router, _, _ := browserFixture(t, "http://127.0.0.1:5173")
	for n := 0; n < 32; n++ {
		path := "/auth/login"
		if n%2 == 1 {
			path = "/auth/browser/login"
		}
		rec := browserRequest(router, "POST", path, "{", nil, "http://127.0.0.1:5173", "")
		want := 400
		if n >= 30 {
			want = 429
		}
		if rec.Code != want {
			t.Fatalf("attempt %d: got %d want %d", n, rec.Code, want)
		}
	}
}

func TestBrowserFailedLogoutRetainsCookie(t *testing.T) {
	origin := "http://127.0.0.1:5173"
	router, r, _ := browserFixture(t, origin)
	cookie, response := loginBrowser(t, router, origin)
	// Authenticate succeeds, then the repository rejects revocation. A separate
	// route exercises Logout with the principal already established by middleware.
	h := NewHandler(&Service{sessions: r})
	router.POST("/revoke-failure", NewHandler(&Service{sessions: r}).RequireSession, func(c *gin.Context) {
		r.err = errors.New("private revocation failure")
		h.BrowserLogout(c)
	})
	rec := browserRequest(router, "POST", "/revoke-failure", "", cookie, origin, response.CSRFToken)
	if rec.Code != 500 || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("failed logout cleared the cookie")
	}
	r.err = nil
	if rec := browserRequest(router, "GET", "/auth/browser/session", "", cookie, "", ""); rec.Code != 200 {
		t.Fatal("failed logout lost the session")
	}
}
