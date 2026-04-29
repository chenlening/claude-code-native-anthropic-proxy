package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsRegister(t *testing.T) {
	m := NewMetrics("anthropic_proxy")

	registry := prometheus.NewRegistry()
	registry.MustRegister(m)

	// Write some data so metrics appear in Gather
	m.RecordRequest("test-model", "test-backend", "test-endpoint", 0.1, true)
	m.SetEndpointEnabled("test-ep", true)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	if len(mfs) == 0 {
		t.Error("Expected metrics to be gathered after recording data")
	}

	// Verify we can find at least our key metrics
	foundRequests := false
	foundEnabled := false
	for _, mf := range mfs {
		name := mf.GetName()
		if name == "anthropic_proxy_requests_total" {
			foundRequests = true
		}
		if name == "anthropic_proxy_endpoint_enabled" {
			foundEnabled = true
		}
	}
	if !foundRequests {
		t.Error("requests_total not found in gathered metrics")
	}
	if !foundEnabled {
		t.Error("endpoint_enabled not found in gathered metrics")
	}
}

func TestMetricsRecordRequest(t *testing.T) {
	m := NewMetrics("test_proxy")
	registry := prometheus.NewRegistry()
	registry.MustRegister(m)

	m.RecordRequest("claude-sonnet-4", "glm-5-turbo", "endpoint-a", 1.5, true)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "test_proxy_requests_total" {
			val := mf.GetMetric()[0].GetCounter().GetValue()
			if val != 1 {
				t.Errorf("requests_total = %v, want 1", val)
			}
		}
	}
}

func TestMetricsRecordEndpointState(t *testing.T) {
	m := NewMetrics("test_proxy")
	registry := prometheus.NewRegistry()
	registry.MustRegister(m)

	m.SetEndpointEnabled("endpoint-a", true)
	m.SetEndpointEnabled("endpoint-b", false)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "test_proxy_endpoint_enabled" {
			for _, metric := range mf.GetMetric() {
				label := metric.GetLabel()[0].GetValue()
				value := metric.GetGauge().GetValue()
				if label == "endpoint-a" && value != 1 {
					t.Errorf("endpoint-a enabled = %v, want 1", value)
				}
				if label == "endpoint-b" && value != 0 {
					t.Errorf("endpoint-b enabled = %v, want 0", value)
				}
			}
		}
	}
}

func TestMetricsSnapshot(t *testing.T) {
	m := NewMetrics("test_snap")

	// Record multiple requests
	m.RecordRequest("claude-sonnet-4", "glm-5-turbo", "gzl", 0.5, true)
	m.RecordRequest("claude-sonnet-4", "glm-5-turbo", "gzl", 1.5, true)
	m.RecordRequest("claude-sonnet-4", "glm-5-turbo", "aliyun", 2.0, true)
	m.RecordRequest("claude-opus-4", "glm-5.1", "gzl", 3.0, false)

	snap := m.Snapshot()

	if snap.TotalRequests != 4 {
		t.Errorf("TotalRequests = %d, want 4", snap.TotalRequests)
	}

	// Check endpoint stats
	if snap.Endpoints["gzl"].Requests != 3 {
		t.Errorf("gzl requests = %d, want 3", snap.Endpoints["gzl"].Requests)
	}
	if snap.Endpoints["gzl"].Failures != 1 {
		t.Errorf("gzl failures = %d, want 1", snap.Endpoints["gzl"].Failures)
	}

	// Check model stats
	if snap.Models["glm-5-turbo"].Requests != 3 {
		t.Errorf("glm-5-turbo requests = %d, want 3", snap.Models["glm-5-turbo"].Requests)
	}

	// Check latency
	if snap.Models["glm-5-turbo"].Latency.MinMs != 500 {
		t.Errorf("glm-5-turbo min_ms = %v, want 500", snap.Models["glm-5-turbo"].Latency.MinMs)
	}
	if snap.Models["glm-5-turbo"].Latency.MaxMs != 2000 {
		t.Errorf("glm-5-turbo max_ms = %v, want 2000", snap.Models["glm-5-turbo"].Latency.MaxMs)
	}

	// Check by_backend
	if len(snap.ByBackend) != 3 {
		t.Errorf("by_backend count = %d, want 3", len(snap.ByBackend))
	}
}
