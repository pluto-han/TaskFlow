// Package telemetry owns a private registry, separate from public API routes.
package telemetry

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	scheduleScans       *prometheus.CounterVec
	scheduleGenerated   prometheus.Counter
	scheduleProcessed   prometheus.Counter
	scheduleDuration    prometheus.Histogram
	scheduleLastSuccess prometheus.Gauge
	registry            *prometheus.Registry
	requests            *prometheus.CounterVec
	duration            *prometheus.HistogramVec
}

func New(pool *pgxpool.Pool, ping func(context.Context) error) *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_http_requests_total", Help: "Completed HTTP requests by bounded method, route template and status class."}, []string{"method", "route", "status_class"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "taskflow_http_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"method", "route"}),
	}
	m.scheduleScans = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_recurrence_scans_total", Help: "Background batch attempts by bounded outcome."}, []string{"outcome"})
	m.scheduleGenerated = prometheus.NewCounter(prometheus.CounterOpts{Name: "taskflow_recurrence_generated_total", Help: "Tasks observed committed by the scheduler; not an accounting ledger."})
	m.scheduleProcessed = prometheus.NewCounter(prometheus.CounterOpts{Name: "taskflow_recurrence_processed_total", Help: "Rules observed advanced by committed scans."})
	m.scheduleDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "taskflow_recurrence_batch_duration_seconds", Help: "Background scan duration.", Buckets: []float64{.01, .05, .1, .5, 1, 2, 5}})
	m.scheduleLastSuccess = prometheus.NewGauge(prometheus.GaugeOpts{Name: "taskflow_recurrence_last_success_timestamp_seconds", Help: "Unix time of last successful scan; zero before first success."})
	m.registry.MustRegister(m.scheduleScans, m.scheduleGenerated, m.scheduleProcessed, m.scheduleDuration, m.scheduleLastSuccess)
	m.registry.MustRegister(m.requests, m.duration, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if pool != nil {
		for name, value := range map[string]func() float64{
			"acquired_connections": func() float64 { return float64(pool.Stat().AcquiredConns()) },
			"idle_connections":     func() float64 { return float64(pool.Stat().IdleConns()) },
			"max_connections":      func() float64 { return float64(pool.Stat().MaxConns()) },
		} {
			m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "taskflow_db_pool_" + name, Help: "PostgreSQL pool " + name + "."}, value))
		}
		m.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "taskflow_db_pool_waits_total", Help: "Acquisitions which found an empty pool."}, func() float64 { return float64(pool.Stat().EmptyAcquireCount()) }))
	}
	if ping != nil {
		m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "taskflow_database_ready", Help: "Whether a bounded database ping succeeds (1 or 0)."}, func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if ping(ctx) != nil {
				return 0
			}
			return 1
		}))
	}
	return m
}

// Observe accepts sanitized method/route values from the HTTP observer, never URLs.
func (m *Metrics) Observe(method, route string, status int, elapsed time.Duration) {
	class := "other"
	if status >= 100 && status < 600 {
		class = strconv.Itoa(status/100) + "xx"
	}
	m.requests.WithLabelValues(method, route, class).Inc()
	m.duration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}

func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{MaxRequestsInFlight: 2, Timeout: 3 * time.Second}))
	return mux
}

// ObserveSchedule uses no user/rule IDs or task contents as labels.
func (m *Metrics) ObserveSchedule(ok bool, processed, generated int, elapsed time.Duration) {
	outcome := "error"
	if ok {
		outcome = "success"
		m.scheduleGenerated.Add(float64(generated))
		m.scheduleProcessed.Add(float64(processed))
		m.scheduleLastSuccess.SetToCurrentTime()
	}
	m.scheduleScans.WithLabelValues(outcome).Inc()
	m.scheduleDuration.Observe(elapsed.Seconds())
}
