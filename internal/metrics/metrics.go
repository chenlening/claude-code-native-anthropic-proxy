package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// LatencyStats holds latency statistics for a group of requests
type LatencyStats struct {
	Count int64   `json:"count"`
	MinMs float64 `json:"min_ms"`
	MaxMs float64 `json:"max_ms"`
	AvgMs float64 `json:"avg_ms"`
}

// BackendStats holds stats for a specific (frontend_model, backend_model, endpoint) combination
type BackendStats struct {
	FrontendModel string       `json:"frontend_model"`
	BackendModel  string       `json:"backend_model"`
	Endpoint      string       `json:"endpoint"`
	Latency       LatencyStats `json:"latency"`
}

// EndpointSnapshot holds per-endpoint summary for health response
type EndpointSnapshot struct {
	Status            string     `json:"status"`
	Requests          int64      `json:"requests"`
	Failures          int64      `json:"failures"`
	ActiveConnections int        `json:"active_connections"`
	LastRequestTime   *time.Time `json:"lastRequestTime,omitempty"`
	LastFailureTime   *time.Time `json:"lastFailureTime,omitempty"`
	LastFailureReason string     `json:"lastFailureReason,omitempty"`
	LastProbeTime     *time.Time `json:"lastProbeTime,omitempty"`
	LastProbeSuccess  *bool      `json:"lastProbeSuccess,omitempty"`
}

// ModelSnapshot holds per-backend-model summary for health response
type ModelSnapshot struct {
	Requests int64       `json:"requests"`
	Latency  LatencyStats `json:"latency"`
}

// Snapshot holds a point-in-time view of all stats
type Snapshot struct {
	TotalRequests int64                       `json:"total_requests"`
	Endpoints     map[string]EndpointSnapshot `json:"endpoints"`
	Models        map[string]ModelSnapshot    `json:"models"`
	ByBackend     []BackendStats              `json:"by_backend"`
}

type backendKey struct {
	FrontendModel string
	BackendModel  string
	Endpoint      string
}

type latencyAccum struct {
	count    int64
	totalSec float64
	minSec   float64
	maxSec   float64
	failures int64
}

// Metrics holds all Prometheus metrics for the proxy
type Metrics struct {
	namespace string

	RequestsTotal       *prometheus.CounterVec
	RequestsByModel     *prometheus.CounterVec
	RequestsByEndpoint  *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	EndpointFailures *prometheus.CounterVec
	EndpointEnabled  *prometheus.GaugeVec

	mu      sync.RWMutex
	latency map[backendKey]*latencyAccum
}

// NewMetrics creates a new Metrics instance
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		namespace: namespace,
		latency:   make(map[backendKey]*latencyAccum),

		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "requests_total",
				Help:      "Total number of requests processed",
			},
			[]string{},
		),

		RequestsByModel: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "requests_by_model",
				Help:      "Number of requests per model",
			},
			[]string{"model"},
		),

		RequestsByEndpoint: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "requests_by_endpoint",
				Help:      "Number of requests per endpoint",
			},
			[]string{"endpoint"},
		),

		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "request_duration_seconds",
				Help:      "Request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"model", "endpoint"},
		),

		EndpointFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "endpoint_failures_total",
				Help:      "Total failures per endpoint",
			},
			[]string{"endpoint"},
		),

		EndpointEnabled: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "endpoint_enabled",
				Help:      "Whether endpoint is enabled (1=enabled, 0=disabled)",
			},
			[]string{"endpoint"},
		),
	}
}

// Describe implements prometheus.Collector
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	m.RequestsTotal.Describe(ch)
	m.RequestsByModel.Describe(ch)
	m.RequestsByEndpoint.Describe(ch)
	m.RequestDuration.Describe(ch)
	m.EndpointFailures.Describe(ch)
	m.EndpointEnabled.Describe(ch)
}

// Collect implements prometheus.Collector
func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	m.RequestsTotal.Collect(ch)
	m.RequestsByModel.Collect(ch)
	m.RequestsByEndpoint.Collect(ch)
	m.RequestDuration.Collect(ch)
	m.EndpointFailures.Collect(ch)
	m.EndpointEnabled.Collect(ch)
}

// RecordRequest records a completed request
func (m *Metrics) RecordRequest(frontendModel, backendModel, endpoint string, duration float64, success bool) {
	m.RequestsTotal.WithLabelValues().Inc()
	m.RequestsByModel.WithLabelValues(frontendModel).Inc()
	m.RequestsByEndpoint.WithLabelValues(endpoint).Inc()
	m.RequestDuration.WithLabelValues(frontendModel, endpoint).Observe(duration)

	if !success {
		m.EndpointFailures.WithLabelValues(endpoint).Inc()
	}

	// Update in-memory latency stats
	key := backendKey{FrontendModel: frontendModel, BackendModel: backendModel, Endpoint: endpoint}
	m.mu.Lock()
	acc, ok := m.latency[key]
	if !ok {
		acc = &latencyAccum{minSec: duration, maxSec: duration}
		m.latency[key] = acc
	}
	acc.count++
	acc.totalSec += duration
	if duration < acc.minSec {
		acc.minSec = duration
	}
	if duration > acc.maxSec {
		acc.maxSec = duration
	}
	if !success {
		acc.failures++
	}
	m.mu.Unlock()
}

// Snapshot returns a point-in-time view of all stats
func (m *Metrics) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := Snapshot{
		Endpoints: make(map[string]EndpointSnapshot),
		Models:    make(map[string]ModelSnapshot),
	}

	for key, acc := range m.latency {
		snap.TotalRequests += acc.count

		eps := snap.Endpoints[key.Endpoint]
		eps.Requests += acc.count
		eps.Failures += acc.failures
		snap.Endpoints[key.Endpoint] = eps

		ms := snap.Models[key.BackendModel]
		ms.Requests += acc.count
		mergeLatency(&ms.Latency, acc)
		snap.Models[key.BackendModel] = ms

		snap.ByBackend = append(snap.ByBackend, BackendStats{
			FrontendModel: key.FrontendModel,
			BackendModel:  key.BackendModel,
			Endpoint:      key.Endpoint,
			Latency:       toLatencyStats(acc),
		})
	}

	return snap
}

func mergeLatency(ls *LatencyStats, acc *latencyAccum) {
	ls.Count += acc.count
	if acc.minSec*1000 < ls.MinMs || ls.MinMs == 0 {
		ls.MinMs = acc.minSec * 1000
	}
	if acc.maxSec*1000 > ls.MaxMs {
		ls.MaxMs = acc.maxSec * 1000
	}
	ls.AvgMs = ls.AvgMs + (acc.totalSec*1000/float64(acc.count)-ls.AvgMs)*(float64(acc.count)/float64(ls.Count))
}

func toLatencyStats(acc *latencyAccum) LatencyStats {
	return LatencyStats{
		Count: acc.count,
		MinMs: acc.minSec * 1000,
		MaxMs: acc.maxSec * 1000,
		AvgMs: acc.totalSec * 1000 / float64(acc.count),
	}
}

// SetEndpointEnabled sets the enabled state for an endpoint
func (m *Metrics) SetEndpointEnabled(endpoint string, enabled bool) {
	value := float64(0)
	if enabled {
		value = 1
	}
	m.EndpointEnabled.WithLabelValues(endpoint).Set(value)
}
