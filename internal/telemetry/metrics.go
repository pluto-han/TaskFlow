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
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func New(pool *pgxpool.Pool, ping func(context.Context) error) *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_http_requests_total", Help: "Completed HTTP requests by bounded method, route template and status class."}, []string{"method", "route", "status_class"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "taskflow_http_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"method", "route"}),
	}
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
