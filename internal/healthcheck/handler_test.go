package healthcheck

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/metrics"
)

func TestHealthHandlerReturnsHealthy(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	m := metrics.NewMetrics("test_health")

	epA := endpoint.NewEndpointState("endpoint-a", "https://api.anthropic.com", "key")
	epB := endpoint.NewEndpointState("endpoint-b", "https://custom.com", "key")
	hm.AddEndpoint(epA)
	hm.AddEndpoint(epB)

	handler := NewHandler(hm, m, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", resp.Status)
	}

	if len(resp.Endpoints) != 2 {
		t.Errorf("Endpoints count = %d, want 2", len(resp.Endpoints))
	}

	if resp.Endpoints["endpoint-a"].Status != "enabled" {
		t.Errorf("endpoint-a status = %q, want enabled", resp.Endpoints["endpoint-a"].Status)
	}
}

func TestHealthHandlerReturnsDegradedWhenSomeDisabled(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 1})
	m := metrics.NewMetrics("test_health")

	epA := endpoint.NewEndpointState("endpoint-a", "https://api.anthropic.com", "key")
	epB := endpoint.NewEndpointState("endpoint-b", "https://custom.com", "key")
	hm.AddEndpoint(epA)
	hm.AddEndpoint(epB)

	epB.Disable()

	handler := NewHandler(hm, m, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("Status = %q, want degraded (some endpoints disabled)", resp.Status)
	}

	if resp.Endpoints["endpoint-b"].Status != "disabled" {
		t.Errorf("endpoint-b status = %q, want disabled", resp.Endpoints["endpoint-b"].Status)
	}
}

func TestHealthHandlerReturnsUnhealthyWhenAllDisabled(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 1})
	m := metrics.NewMetrics("test_health")

	epA := endpoint.NewEndpointState("endpoint-a", "https://api.anthropic.com", "key")
	hm.AddEndpoint(epA)

	epA.Disable()

	handler := NewHandler(hm, m, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want 503", rec.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if resp.Status != "unhealthy" {
		t.Errorf("Status = %q, want unhealthy", resp.Status)
	}
}

func TestHealthHandlerReturnsStats(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	m := metrics.NewMetrics("test_stats")

	ep := endpoint.NewEndpointState("gzl", "https://open.bigmodel.cn", "key")
	hm.AddEndpoint(ep)

	// Record some requests
	m.RecordRequest("claude-sonnet-4", "glm-5-turbo", "gzl", 0.5, true)
	m.RecordRequest("claude-sonnet-4", "glm-5-turbo", "gzl", 1.5, true)

	handler := NewHandler(hm, m, nil)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if resp.TotalRequests != 2 {
		t.Errorf("TotalRequests = %d, want 2", resp.TotalRequests)
	}

	if resp.Endpoints["gzl"].Requests != 2 {
		t.Errorf("gzl requests = %d, want 2", resp.Endpoints["gzl"].Requests)
	}

	if resp.Models["glm-5-turbo"].Requests != 2 {
		t.Errorf("glm-5-turbo requests = %d, want 2", resp.Models["glm-5-turbo"].Requests)
	}

	if len(resp.ByBackend) != 1 {
		t.Fatalf("by_backend count = %d, want 1", len(resp.ByBackend))
	}

	if resp.ByBackend[0].BackendModel != "glm-5-turbo" {
		t.Errorf("backend_model = %q, want glm-5-turbo", resp.ByBackend[0].BackendModel)
	}
}
