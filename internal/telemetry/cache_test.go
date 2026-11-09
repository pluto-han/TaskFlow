package telemetry_test

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pluto-han/taskflow/internal/telemetry"
)

func TestCacheMetricsBoundedAndConcurrent(t *testing.T) {
	m := telemetry.New(nil, nil)
	m.SetCacheEnabled(true)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.ObserveTodayCache("hit")
			m.ObserveTodayCacheFill("success")
			m.ObserveTodayCacheOperation("get", true, time.Millisecond)
		}()
	}
	wg.Wait()
	m.ObserveTodayCache("private-user-id")
	m.ObserveTodayCacheFill("private-date")
	m.ObserveTodayCacheOperation("taskflow:cache:private-key", false, -time.Second)
	m.ObserveTodayCacheOperation("delete", false, time.Millisecond)
	result := httptest.NewRecorder()
	m.Handler().ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	text := result.Body.String()
	for _, value := range []string{
		"taskflow_today_cache_enabled 1",
		`taskflow_today_cache_reads_total{outcome="hit"} 20`,
		`taskflow_today_cache_reads_total{outcome="other"} 1`,
		`taskflow_today_cache_fills_total{outcome="success"} 20`,
		`taskflow_today_cache_fills_total{outcome="other"} 1`,
		`taskflow_today_cache_operations_total{operation="get",outcome="success"} 20`,
		`taskflow_today_cache_operations_total{operation="delete",outcome="error"} 1`,
		`taskflow_today_cache_operation_duration_seconds_count{operation="get"} 20`,
	} {
		if !strings.Contains(text, value) {
			t.Fatal("missing", value)
		}
	}
	for _, secret := range []string{"private-user-id", "private-date", "private-key", "owner_id=", "user_id=", "plan_date=", "key="} {
		if strings.Contains(text, secret) {
			t.Fatal("sensitive metric label")
		}
	}
	m.SetCacheEnabled(false)
	result = httptest.NewRecorder()
	m.Handler().ServeHTTP(result, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(result.Body.String(), "taskflow_today_cache_enabled 0") {
		t.Fatal("cache toggle missing")
	}
}
