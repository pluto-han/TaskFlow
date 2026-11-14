package cache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	interfaceshttptask "github.com/pluto-han/taskflow/internal/interfaces/http/task"
)

type cacheSample struct {
	Status       int     `json:"status"`
	Milliseconds float64 `json:"milliseconds"`
	StartLagMS   float64 `json:"start_lag_ms"`
}

type cacheComparison struct {
	Group            string        `json:"group"`
	Scenario         string        `json:"scenario"`
	Requests         int           `json:"requests"`
	Successes        int           `json:"successes"`
	Rejected         int           `json:"http_429"`
	Errors           int           `json:"errors"`
	TargetSQL        int64         `json:"target_list_sql"`
	AuthSQL          int64         `json:"session_sql"`
	Seconds          float64       `json:"seconds"`
	SuccessPerSecond float64       `json:"success_per_second"`
	P95MS            float64       `json:"success_p95_ms"`
	P99MS            float64       `json:"success_p99_ms"`
	MaxStartLagMS    float64       `json:"max_start_lag_ms"`
	FirstTargetSQL   int64         `json:"first_request_target_sql"`
	Samples          []cacheSample `json:"samples"`
}

// No bodies, identities, credentials, URLs or keys enter the saved report.
func sampleToday(f *cacheHTTPFixture, token string, count int, due time.Time, firstID ...string) cacheSample {
	lag := max(0.0, time.Since(due).Seconds()*1000)
	started := time.Now()
	request, _ := http.NewRequest("GET", f.server.URL+"/tasks/today?limit=20", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := f.http.Do(request)
	sample := cacheSample{StartLagMS: lag}
	if err == nil {
		var page interfaceshttptask.TodayPageResponse
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page)
		_ = response.Body.Close()
		sample.Status = response.StatusCode
		if response.StatusCode == 200 && (decodeErr != nil || len(page.Items) != count || page.PlanDate != "2026-11-05" || (len(firstID) > 0 && (len(page.Items) == 0 || page.Items[0].ID != firstID[0]))) {
			sample.Status = 0
		}
	}
	sample.Milliseconds = time.Since(started).Seconds() * 1000
	return sample
}

func summarize(row *cacheComparison, started time.Time, reads, auth int64, f *cacheHTTPFixture) {
	row.Requests = len(row.Samples)
	row.TargetSQL = f.repo.reads.Load() - reads
	row.AuthSQL = f.auth.reads.Load() - auth
	row.Seconds = time.Since(started).Seconds()
	var latencies []float64
	for _, sample := range row.Samples {
		row.MaxStartLagMS = max(row.MaxStartLagMS, sample.StartLagMS)
		switch sample.Status {
		case 200:
			row.Successes++
			latencies = append(latencies, sample.Milliseconds)
		case 429:
			row.Rejected++
		default:
			row.Errors++
		}
	}
	row.SuccessPerSecond = float64(row.Successes) / row.Seconds
	slices.Sort(latencies)
	if len(latencies) > 0 {
		row.P95MS = latencies[int(math.Ceil(float64(len(latencies))*.95))-1]
		row.P99MS = latencies[int(math.Ceil(float64(len(latencies))*.99))-1]
	}
}

func seedPerformance(t *testing.T, f *cacheHTTPFixture, label string) ([2]domainaccount.UserID, [2]string) {
	t.Helper()
	var owners [2]domainaccount.UserID
	var tokens [2]string
	for i := range owners {
		owners[i], tokens[i], _ = f.register(t, label+fmt.Sprint(i))
	}
	date, yesterday := "2026-11-05", "2026-11-04"
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for _, seed := range []struct {
		owner int
		name  string
		count int
		date  *string
	}{
		{0, "today-a", 24, &date}, {1, "today-b", 8, &date}, {0, "history", 3, &yesterday}, {0, "ordinary", 4, nil},
	} {
		for i := 0; i < seed.count; i++ {
			item := domaintask.Task{ID: fmt.Sprintf("%s-%s-%s-%d", f.marker, label, seed.name, i), Title: "Baseline task", Priority: i%3 + 1,
				Completed: i%4 == 0, CreatedAt: now.Add(time.Duration(i) * time.Microsecond), PlanDate: seed.date}
			if i%2 == 0 {
				due := now.Add(time.Hour)
				item.DueDate = &due
			}
			if err := f.repo.Save(context.Background(), owners[seed.owner], &item); err != nil {
				t.Fatal("cannot seed isolated comparison")
			}
		}
	}
	return owners, tokens
}

// Opt-in only. Normal CI runs correctness tests with race detection; timings are
// measured without -race, using the exact same guarded disposable fixture.
func TestRedisPostgresPerformanceComparison(t *testing.T) {
	if os.Getenv("TASKFLOW_CACHE_PERFORMANCE") != "true" {
		t.Skip("run make compare-cache for the isolated measured workload")
	}
	var rows []cacheComparison
	for _, group := range []string{"disabled", "cold-to-warm", "hot", "paused"} {
		t.Run(group, func(t *testing.T) {
			f := newCacheHTTPFixture(t, "performance")
			owners, tokens := seedPerformance(t, f, group)
			if group == "disabled" {
				f.service.WithTodayCache(nil)
				f.metrics.SetCacheEnabled(false)
			}
			if group == "paused" {
				controlRedis(t, f.marker, "pause")
				t.Cleanup(func() { controlRedis(t, f.marker, "unpause") })
			}
			for _, scenario := range []string{"same-user", "alternating-two-users"} {
				if group != "paused" {
					for _, owner := range owners {
						if err := f.client.Delete(context.Background(), todayFixtureKey(owner)); err != nil {
							t.Fatal("cannot reset owned comparison key")
						}
					}
				}
				if group == "hot" {
					_ = f.today(t, tokens[0])
					if scenario == "alternating-two-users" {
						_ = f.today(t, tokens[1])
					}
				}
				row := cacheComparison{Group: group, Scenario: scenario}
				reads, auth := f.repo.reads.Load(), f.auth.reads.Load()
				started := time.Now()
				for i := range 100 {
					due := started.Add(time.Duration(i) * 100 * time.Millisecond)
					time.Sleep(max(0, time.Until(due)))
					index, count := 0, 20
					if scenario == "alternating-two-users" && i%2 == 1 {
						index, count = 1, 8
					}
					row.Samples = append(row.Samples, sampleToday(f, tokens[index], count, due))
					if i == 0 {
						row.FirstTargetSQL = f.repo.reads.Load() - reads
					}
				}
				summarize(&row, started, reads, auth, f)
				rows = append(rows, row)
				t.Logf("%s/%s success=%d 429=%d list_SQL=%d auth_SQL=%d p95=%.3fms p99=%.3fms lag_max=%.3fms", group, scenario, row.Successes, row.Rejected, row.TargetSQL, row.AuthSQL, row.P95MS, row.P99MS, row.MaxStartLagMS)
				if row.Errors != 0 || row.Rejected != 0 || row.AuthSQL != 100 {
					t.Fatal("comparison needs 100 successful authenticated requests")
				}
				switch group {
				case "disabled", "paused":
					if row.TargetSQL != 100 {
						t.Fatal("fallback query count changed")
					}
				case "cold-to-warm":
					expected := int64(1)
					if scenario == "alternating-two-users" {
						expected = 2
					}
					if row.TargetSQL != expected || row.FirstTargetSQL != 1 {
						t.Fatal("cold fills changed; inspect scheduling and TTL before comparing")
					}
				case "hot":
					if row.TargetSQL > 20 {
						t.Fatal("target list SQL reduction below 80%; inspect TTL/scheduler lag")
					}
				}
			}
			scrape := httptest.NewRecorder()
			f.metrics.Handler().ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))
			saveComparison(t, group+"-metrics.txt", []byte(scrape.Body.String()))
		})
	}
	// Natural expiry uses advancing elapsed time, not an artificially frozen clock.
	t.Run("expiry-and-mixed-writes", func(t *testing.T) {
		f := newCacheHTTPFixture(t, "performance")
		_, tokens := seedPerformance(t, f, "extra")
		_ = f.today(t, tokens[0])
		time.Sleep(16 * time.Second)
		row := cacheComparison{Group: "expired", Scenario: "eight-concurrent-http-reads"}
		reads, auth := f.repo.reads.Load(), f.auth.reads.Load()
		started := time.Now()
		samples := make(chan cacheSample, 8)
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() { defer wg.Done(); samples <- sampleToday(f, tokens[0], 20, started) }()
		}
		wg.Wait()
		close(samples)
		for sample := range samples {
			row.Samples = append(row.Samples, sample)
		}
		summarize(&row, started, reads, auth, f)
		rows = append(rows, row)
		if row.Successes != 8 || row.AuthSQL != 8 || row.TargetSQL < 1 || row.TargetSQL > 8 {
			t.Fatal("expiry workload failed")
		}
		row = cacheComparison{Group: "mixed", Scenario: "ten-create-read-read-read-cycles"}
		reads, auth = f.repo.reads.Load(), f.auth.reads.Load()
		started = time.Now()
		for i := range 10 {
			time.Sleep(max(0, time.Until(started.Add(time.Duration(i)*time.Second))))
			item := f.create(t, tokens[0], "Measured write")
			for range 3 {
				row.Samples = append(row.Samples, sampleToday(f, tokens[0], 20, time.Now(), item.ID))
			}
		}
		summarize(&row, started, reads, auth, f)
		rows = append(rows, row)
		if row.Successes != 30 || row.TargetSQL != 10 || row.AuthSQL != 40 {
			t.Fatal("mixed workload did not invalidate once per committed write")
		}
	})
	t.Run("rate-limit-visible", func(t *testing.T) {
		f := newCacheHTTPFixture(t, "performance")
		_, tokens := seedPerformance(t, f, "limited")
		_ = f.today(t, tokens[0])
		row := cacheComparison{Group: "overload-probe", Scenario: "320-unpaced-requests-existing-go-limit"}
		reads, auth := f.repo.reads.Load(), f.auth.reads.Load()
		started := time.Now()
		for range 320 {
			row.Samples = append(row.Samples, sampleToday(f, tokens[0], 20, time.Now()))
		}
		summarize(&row, started, reads, auth, f)
		rows = append(rows, row)
		if row.Rejected == 0 || row.Errors != 0 || row.AuthSQL != int64(row.Successes) {
			t.Fatal("429 accounting or existing limit changed")
		}
	})
	raw, err := json.MarshalIndent(struct {
		Go   string            `json:"go"`
		OS   string            `json:"os"`
		Arch string            `json:"arch"`
		CPUs int               `json:"gomaxprocs"`
		Rate int               `json:"comparison_requests_per_second"`
		Rows []cacheComparison `json:"rows"`
	}{runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), 10, rows}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	saveComparison(t, "comparison.json", append(raw, '\n'))
}

func saveComparison(t *testing.T, name string, data []byte) {
	t.Helper()
	dir := os.Getenv("TASKFLOW_CACHE_REPORT_DIR")
	if dir == "" {
		t.Fatal("comparison report directory missing")
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal("cannot save sanitized comparison")
	}
}
