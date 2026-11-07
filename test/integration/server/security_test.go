package server_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pluto-han/taskflow/internal/server"
)

// Exercise the actual production registration/login routes, before any expensive
// password work. Invalid JSON fails normally until the middleware budget is spent.
func TestAuthRateLimitsCannotBeBypassedByForwardedIP(t *testing.T) {
	for _, tc := range []struct {
		path  string
		limit int
	}{{"/auth/register", 10}, {"/auth/login", 20}} {
		t.Run(tc.path, func(t *testing.T) {
			r := server.NewRouter(server.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			for n := 0; n <= tc.limit; n++ {
				req := httptest.NewRequest("POST", tc.path, strings.NewReader("not JSON"))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", n))
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				expected := 400
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
