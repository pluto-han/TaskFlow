package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
)

func TestObservationAndRecoveryNeverLogCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	router := gin.New()
	router.Use(interfaceshttpmiddleware.Observe(slog.New(slog.NewJSONHandler(&output, nil))))
	router.POST("/secret/:id", func(c *gin.Context) {
		if interfaceshttpmiddleware.RequestID(c.Request.Context()) != c.Writer.Header().Get("X-Request-ID") {
			t.Error("context ID differs")
		}
		panic("panic-private-value")
	})
	router.NoRoute(func(c *gin.Context) { httpapi.WriteError(c, 404, "ROUTE_NOT_FOUND", "Route not found") })
	ids := map[string]bool{}
	for _, path := range []string{"/secret/path-private-value?token=query-private-value", "/unknown-private-value?password=query-private-value"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"password":"body-private-value"}`))
		req.Header.Set("Authorization", "Bearer header-private-value")
		req.Header.Set("Cookie", "cookie-private-value")
		req.Header.Set("X-Request-ID", "client-private-value")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-ID")
		if id == "" || ids[id] {
			t.Fatal("missing/duplicate request ID")
		}
		ids[id] = true
		var response httpapi.ErrorResponse
		if json.Unmarshal(rec.Body.Bytes(), &response) != nil {
			t.Fatal("non-JSON error")
		}
		if strings.HasPrefix(path, "/secret/") && (rec.Code != 500 || response.Error.Code != "INTERNAL_ERROR") {
			t.Fatal("panic response")
		}
	}
	for _, secret := range []string{"path-private-value", "query-private-value", "body-private-value", "header-private-value", "cookie-private-value", "client-private-value", "unknown-private-value", "panic-private-value"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("log contains %s", secret)
		}
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatal("one log per request required")
	}
	for i, line := range lines {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			t.Fatal("invalid structured log")
		}
		if !ids[entry["request_id"].(string)] {
			t.Fatal("log ID mismatch")
		}
		if i == 0 && (entry["route"] != "/secret/:id" || entry["panicked"] != true || entry["status"] != float64(500)) {
			t.Fatal(entry)
		}
	}
}

func TestRecoveryAfterResponseDoesNotAppendJSON(t *testing.T) {
	var output bytes.Buffer
	r := gin.New()
	r.Use(interfaceshttpmiddleware.Observe(slog.New(slog.NewJSONHandler(&output, nil))))
	r.GET("/written", func(c *gin.Context) { c.String(200, "partial"); panic("secret") })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/written", nil))
	if rec.Body.String() != "partial" || !strings.Contains(output.String(), `"panicked":true`) {
		t.Fatal("committed response corrupted or panic not recorded")
	}
}

func TestHealthProbes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ping   func(context.Context) error
		status int
	}{
		{"healthy", func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Second {
				t.Error("unbounded ping")
			}
			return nil
		}, 200},
		{"failed", func(context.Context) error { return errors.New("postgres://secret") }, 503},
		{"missing", nil, 503},
		{"timeout", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			calls := 0
			ping := tc.ping
			if ping != nil {
				ping = func(ctx context.Context) error { calls++; return tc.ping(ctx) }
			}
			interfaceshttprouter.MountHealth(r, ping)
			for _, path := range []string{"/health", "/health/live"} {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
				if rec.Code != 200 || calls != 0 {
					t.Fatal("liveness touched dependency")
				}
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", "/health/ready", nil))
			if rec.Code != tc.status || strings.Contains(rec.Body.String(), "secret") || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("readiness: %d %s", rec.Code, rec.Body.String())
			}
			if tc.ping != nil && calls != 1 {
				t.Fatal("unexpected ping count")
			}
		})
	}
	// Cancellation must reach the dependency, not be replaced by Background.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := gin.New()
	interfaceshttprouter.MountHealth(r, func(c context.Context) error {
		if !errors.Is(c.Err(), context.Canceled) {
			t.Error("request cancellation lost")
		}
		return c.Err()
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/health/ready", nil).WithContext(ctx))
	if rec.Code != 503 {
		t.Fatal(rec.Code)
	}
}
