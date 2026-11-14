package inframetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Fixed label vocabularies bound series count, even if a caller passes bad data.
func (m *Metrics) initCache() {
	m.cacheEnabled = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "taskflow_today_cache_enabled",
		Help: "Whether the optional Today cache is configured; not a Redis health probe.",
	})
	m.cacheReads = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "taskflow_today_cache_reads_total",
		Help: "Today read path decisions; requests rejected before cache selection are excluded.",
	}, []string{"outcome"})
	m.cacheFills = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "taskflow_today_cache_fills_total",
		Help: "Cache fill results; skipped means the snapshot could not be safely stored.",
	}, []string{"outcome"})
	m.cacheOperations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "taskflow_today_cache_operations_total",
		Help: "Redis GET/SET/DELETE attempts by bounded outcome; GET misses count as successful commands.",
	}, []string{
		"operation",
		"outcome",
	})
	m.cacheDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "taskflow_today_cache_operation_duration_seconds",
		Help:    "End-to-end Redis operation time, including pool wait and network errors.",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25},
	}, []string{"operation"})
	for _, outcome := range []string{"hit", "miss", "bypass", "error", "other"} {
		m.cacheReads.WithLabelValues(outcome)
	}
	for _, outcome := range []string{"success", "error", "skipped", "other"} {
		m.cacheFills.WithLabelValues(outcome)
	}
	for _, operation := range []string{"get", "set", "delete", "other"} {
		m.cacheDuration.WithLabelValues(operation)
		for _, outcome := range []string{"success", "error"} {
			m.cacheOperations.WithLabelValues(operation, outcome)
		}
	}
	m.registry.MustRegister(m.cacheEnabled, m.cacheReads, m.cacheFills, m.cacheOperations, m.cacheDuration)
}

func (m *Metrics) SetCacheEnabled(enabled bool) {
	value := 0.0
	if enabled {
		value = 1
	}
	m.cacheEnabled.Set(value)
}

func (m *Metrics) ObserveTodayCache(outcome string) {
	switch outcome {
	case "hit", "miss", "bypass", "error":
	default:
		outcome = "other"
	}
	m.cacheReads.WithLabelValues(outcome).Inc()
}

func (m *Metrics) ObserveTodayCacheFill(outcome string) {
	switch outcome {
	case "success", "error", "skipped":
	default:
		outcome = "other"
	}
	m.cacheFills.WithLabelValues(outcome).Inc()
}

func (m *Metrics) ObserveTodayCacheOperation(operation string, ok bool, elapsed time.Duration) {
	switch operation {
	case "get", "set", "delete":
	default:
		operation = "other"
	}
	outcome := "error"
	if ok {
		outcome = "success"
	}
	m.cacheOperations.WithLabelValues(operation, outcome).Inc()
	if elapsed < 0 {
		elapsed = 0
	}
	m.cacheDuration.WithLabelValues(operation).Observe(elapsed.Seconds())
}
