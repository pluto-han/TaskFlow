// Package telemetry owns a private registry, separate from public API routes.
package telemetry

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	cacheEnabled        prometheus.Gauge
	cacheReads          *prometheus.CounterVec
	cacheFills          *prometheus.CounterVec
	cacheOperations     *prometheus.CounterVec
	cacheDuration       *prometheus.HistogramVec
	scheduleScans       *prometheus.CounterVec
	scheduleGenerated   prometheus.Counter
	scheduleProcessed   prometheus.Counter
	scheduleDuration    prometheus.Histogram
	scheduleLastSuccess prometheus.Gauge
	schedulerEnabled    prometheus.Gauge
	senderEnabled       prometheus.Gauge
	senderLastSuccess   prometheus.Gauge
	senderPolls         *prometheus.CounterVec
	senderResults       *prometheus.CounterVec
	leaseReclaims       prometheus.Counter
	registry            *prometheus.Registry
	requests            *prometheus.CounterVec
	duration            *prometheus.HistogramVec
}

func New(pool *pgxpool.Pool, ping func(context.Context) error) *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_http_requests_total", Help: "Completed HTTP requests by bounded method, route template and status class."}, []string{"method", "route", "status_class"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "taskflow_http_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"method", "route"}),
	}
	m.initCache()
	m.scheduleScans = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_recurrence_scans_total", Help: "Background batch attempts by bounded outcome."}, []string{"outcome"})
	m.scheduleGenerated = prometheus.NewCounter(prometheus.CounterOpts{Name: "taskflow_recurrence_generated_total", Help: "Tasks observed committed by the scheduler; not an accounting ledger."})
	m.scheduleProcessed = prometheus.NewCounter(prometheus.CounterOpts{Name: "taskflow_recurrence_processed_total", Help: "Rules observed advanced by committed scans."})
	m.scheduleDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "taskflow_recurrence_batch_duration_seconds", Help: "Background scan duration.", Buckets: []float64{.01, .05, .1, .5, 1, 2, 5}})
	m.scheduleLastSuccess = prometheus.NewGauge(prometheus.GaugeOpts{Name: "taskflow_recurrence_last_success_timestamp_seconds", Help: "Unix time of last successful scan; zero before first success."})
	m.schedulerEnabled = prometheus.NewGauge(prometheus.GaugeOpts{Name: "taskflow_recurrence_scheduler_enabled", Help: "Whether the recurrence scheduler is enabled."})
	m.senderEnabled = prometheus.NewGauge(prometheus.GaugeOpts{Name: "taskflow_notification_sender_enabled", Help: "Whether the notification sender is enabled."})
	m.senderLastSuccess = prometheus.NewGauge(prometheus.GaugeOpts{Name: "taskflow_notification_last_success_timestamp_seconds", Help: "Unix time of last successful sender poll, including idle polls."})
	m.senderPolls = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_notification_polls_total", Help: "Sender polls by outcome."}, []string{"outcome"})
	m.senderResults = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "taskflow_notification_send_total", Help: "Discord send attempts by bounded outcome; not an accounting ledger."}, []string{"outcome"})
	m.leaseReclaims = prometheus.NewCounter(prometheus.CounterOpts{Name: "taskflow_notification_lease_reclaims_total", Help: "Expired notification leases reclaimed."})
	for _, outcome := range []string{"success", "error"} {
		m.senderPolls.WithLabelValues(outcome)
	}
	for _, outcome := range []string{"success", "rate_limited", "rejected", "server_error", "network_error"} {
		m.senderResults.WithLabelValues(outcome)
	}
	m.registry.MustRegister(m.scheduleScans, m.scheduleGenerated, m.scheduleProcessed, m.scheduleDuration, m.scheduleLastSuccess, m.schedulerEnabled, m.senderEnabled, m.senderLastSuccess, m.senderPolls, m.senderResults, m.leaseReclaims)
	m.registry.MustRegister(m.requests, m.duration, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if pool != nil {
		// Scrapes are bounded; NaN distinguishes a failed query from an empty queue.
		queue := func(age bool) float64 {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var count int64
			var seconds float64
			err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(EXTRACT(EPOCH FROM (now()-min(o.next_attempt_at))),0)::float8
FROM notification_outbox o JOIN recurrence_rules r ON r.id=o.rule_id AND r.owner_id=o.owner_id
JOIN discord_bindings b ON b.owner_id=o.owner_id AND b.channel_id=o.channel_id AND b.version=o.binding_version
WHERE (o.status IN ('pending','retrying') OR (o.status='leased' AND o.lease_until<=now()))
AND o.next_attempt_at<=now() AND o.expires_at>now() AND o.attempts<8
AND r.deleted_at IS NULL AND NOT r.paused AND r.reminder_enabled
AND NOT EXISTS(SELECT 1 FROM notification_cooldowns c WHERE c.scope IN ('global','channel:'||o.channel_id) AND c.until_at>now())`).Scan(&count, &seconds)
			if err != nil {
				return math.NaN()
			}
			if age {
				return seconds
			}
			return float64(count)
		}
		m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "taskflow_notification_ready_events", Help: "Currently eligible unsent events; excludes inactive rules, expired events and global/channel cooldowns."}, func() float64 { return queue(false) }))
		m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "taskflow_notification_oldest_ready_age_seconds", Help: "Seconds since oldest eligible event became due; zero when empty."}, func() float64 { return queue(true) }))
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

func (m *Metrics) SetWorkers(scheduler, sender bool) {
	if scheduler {
		m.schedulerEnabled.Set(1)
	} else {
		m.schedulerEnabled.Set(0)
	}
	if sender {
		m.senderEnabled.Set(1)
	} else {
		m.senderEnabled.Set(0)
	}
}

func (m *Metrics) ObserveNotificationPoll(ok bool) {
	outcome := "error"
	if ok {
		outcome = "success"
		m.senderLastSuccess.SetToCurrentTime()
	}
	m.senderPolls.WithLabelValues(outcome).Inc()
}

func (m *Metrics) ObserveNotificationSend(status int, failed bool) {
	outcome := "rejected"
	switch {
	case failed:
		outcome = "network_error"
	case status >= 200 && status < 300:
		outcome = "success"
	case status == 429:
		outcome = "rate_limited"
	case status >= 500:
		outcome = "server_error"
	}
	m.senderResults.WithLabelValues(outcome).Inc()
}

func (m *Metrics) ObserveNotificationReclaim() { m.leaseReclaims.Inc() }

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
