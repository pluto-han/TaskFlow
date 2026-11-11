package telemetry_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/telemetry"
)

func TestMetricsSanitizeAndRecover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := telemetry.New(nil, func(context.Context) error { return errors.New("secret-dsn") })
	r := gin.New()
	r.Use(httpapi.Observe(slog.New(slog.NewTextHandler(io.Discard, nil)), m))
	r.GET("/tasks/:id", func(c *gin.Context) { panic("private-password") })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/tasks/private-id?token=private-token", nil))
		}()
	}
	wg.Wait()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PRIVATE-METHOD", "/private-missing", nil))
	result := httptest.NewRecorder()
	m.Handler().ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	text := result.Body.String()
	for _, secret := range []string{"private-id", "private-token", "private-password", "secret-dsn", "PRIVATE-METHOD", "private-missing"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	for _, expected := range []string{`taskflow_http_requests_total{method="GET",route="/tasks/:id",status_class="5xx"} 20`, `method="OTHER",route="<unmatched>",status_class="4xx"`, "taskflow_database_ready 0"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	// The public router does not expose the registry.
	result = httptest.NewRecorder()
	r.ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	if result.Code != 404 {
		t.Fatal("metrics exposed on public router")
	}
}

func TestScrapeProbeHasDeadlineAndRegistryIsIndependent(t *testing.T) {
	m := telemetry.New(nil, func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing deadline")
		}
		return nil
	})
	other := telemetry.New(nil, nil)
	result := httptest.NewRecorder()
	m.Handler().ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(result.Body.String(), "taskflow_database_ready 1") {
		t.Fatal("missing readiness")
	}
	result = httptest.NewRecorder()
	other.Handler().ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(result.Body.String(), "taskflow_database_ready") {
		t.Fatal("shared registry")
	}
	result = httptest.NewRecorder()
	m.Handler().ServeHTTP(result, httptest.NewRequest("POST", "/metrics", nil))
	if result.Code != 405 {
		t.Fatal("write method accepted")
	}
}

func TestSchedulerMetrics(t *testing.T) {
	m := telemetry.New(nil, nil)
	m.ObserveSchedule(true, 3, 2, time.Millisecond)
	m.ObserveSchedule(false, 5, 5, time.Millisecond)
	response := httptest.NewRecorder()
	m.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	for _, expected := range []string{
		`taskflow_recurrence_scans_total{outcome="success"} 1`,
		`taskflow_recurrence_scans_total{outcome="error"} 1`,
		"taskflow_recurrence_generated_total 2",
		"taskflow_recurrence_processed_total 3",
		"taskflow_recurrence_last_success_timestamp_seconds",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatal(expected)
		}
	}
}

func TestNotificationMetricsAreBounded(t *testing.T) {
	m := telemetry.New(nil, nil)
	m.SetWorkers(true, false)
	m.ObserveNotificationPoll(true)
	m.ObserveNotificationPoll(false)
	m.ObserveNotificationReclaim()
	for _, status := range []int{200, 429, 403, 500, 999} {
		m.ObserveNotificationSend(status, false)
	}
	m.ObserveNotificationSend(0, true)
	r := httptest.NewRecorder()
	m.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/metrics", nil))
	for _, expected := range []string{
		"taskflow_recurrence_scheduler_enabled 1", "taskflow_notification_sender_enabled 0",
		`taskflow_notification_polls_total{outcome="success"} 1`,
		`taskflow_notification_polls_total{outcome="error"} 1`,
		`taskflow_notification_send_total{outcome="success"} 1`,
		`taskflow_notification_send_total{outcome="rate_limited"} 1`,
		`taskflow_notification_send_total{outcome="rejected"} 1`,
		`taskflow_notification_send_total{outcome="server_error"} 2`,
		`taskflow_notification_send_total{outcome="network_error"} 1`,
		"taskflow_notification_lease_reclaims_total 1",
	} {
		if !strings.Contains(r.Body.String(), expected) {
			t.Fatal("missing", expected)
		}
	}
	for _, forbidden := range []string{"user_id=", "channel_id=", "rule_id="} {
		if strings.Contains(r.Body.String(), forbidden) {
			t.Fatal("sensitive label", forbidden)
		}
	}
}

func TestEmailMetricLabelsAreBounded(t *testing.T) {
	m := telemetry.New(nil, nil)
	secret := "recipient@example.com/private-capability"
	m.MailResult(secret)
	m.MailResult("success")
	result := httptest.NewRecorder()
	m.Handler().ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	text := result.Body.String()
	if strings.Contains(text, secret) {
		t.Fatal("sensitive mail metric label")
	}
	for _, expected := range []string{`taskflow_email_send_total{outcome="error"} 1`, `taskflow_email_send_total{outcome="success"} 1`} {
		if !strings.Contains(text, expected) {
			t.Fatal("missing bounded mail result")
		}
	}
}
