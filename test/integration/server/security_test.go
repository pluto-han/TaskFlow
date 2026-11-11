package server_test

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/server"
)

// Exercise the actual production registration/login routes, before any expensive
// password work. Invalid JSON fails normally until the middleware budget is spent.
func TestAuthRateLimitsCannotBeBypassedByForwardedIP(t *testing.T) {
	for _, tc := range []struct {
		path          string
		limit         int
		initialStatus int
	}{{"/auth/register", 10, 400}, {"/auth/login", 20, 400}, {"/auth/password/change", 5, 401}, {"/auth/email/verification", 10, 503}, {"/auth/email/verification/confirm", 10, 503}, {"/auth/password/reset/request", 10, 503}, {"/auth/password/reset/confirm", 10, 503}} {
		t.Run(tc.path, func(t *testing.T) {
			r := server.NewRouter(server.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			for n := 0; n <= tc.limit; n++ {
				req := httptest.NewRequest("POST", tc.path, strings.NewReader("not JSON"))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", n))
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				expected := tc.initialStatus
				if n == tc.limit {
					expected = 429
				}
				if rec.Code != expected {
					t.Fatalf("attempt %d: got %d want %d", n, rec.Code, expected)
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("auth error cacheable")
				}
			}
		})
	}
}

func TestBrowserAndBearerLoginSharePeerBudget(t *testing.T) {
	r := server.NewRouter(server.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Security: httpapi.SecurityOptions{PublicOrigin: "http://127.0.0.1:5173"}})
	for n := 0; n < 22; n++ {
		path := "/auth/login"
		if n%2 == 1 {
			path = "/auth/browser/login"
		}
		req := httptest.NewRequest("POST", path, strings.NewReader("not JSON"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:5173")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", n))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		want := 400
		if n >= 20 {
			want = 429
		}
		if rec.Code != want {
			t.Fatalf("attempt %d (%s): got %d want %d", n, path, rec.Code, want)
		}
	}
}

func TestBrowserLoginBodyLimitMatchesLegacy(t *testing.T) {
	for _, path := range []string{"/auth/login", "/auth/browser/login", "/auth/password/change", "/auth/email/verification", "/auth/email/verification/confirm", "/auth/password/reset/request", "/auth/password/reset/confirm"} {
		r := server.NewRouter(server.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		req := httptest.NewRequest("POST", path, strings.NewReader(strings.Repeat("x", 4097)))
		req.Header.Set("Origin", "http://127.0.0.1:5173")
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 413 {
			t.Fatalf("%s: got %d", path, rec.Code)
		}
	}
}

func TestBrowserLoginUsesTrustedTLSBoundary(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("127.0.0.1/32")
	r := server.NewRouter(server.Dependencies{Security: httpapi.SecurityOptions{Production: true, PublicOrigin: "https://tasks.example.com", TrustedProxies: []*net.IPNet{trusted}}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, tc := range []struct {
		peer, proto string
		want        int
	}{
		{"192.0.2.1:1234", "https", 403},
		{"127.0.0.1:1234", "http", 403},
		{"127.0.0.1:1234", "https", 400},
	} {
		req := httptest.NewRequest("POST", "/auth/browser/login", strings.NewReader("{"))
		req.RemoteAddr = tc.peer
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://tasks.example.com")
		req.Header.Set("X-Forwarded-Proto", tc.proto)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("peer=%s proto=%s status=%d", tc.peer, tc.proto, rec.Code)
		}
	}
}
