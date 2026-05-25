package endpoint

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthManagerRecordFailureDisablesEndpoint(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     3,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	ep := NewEndpointState("endpoint-a", "https://api.anthropic.com", "key", "")
	hm.AddEndpoint(ep)

	for i := 0; i < 3; i++ {
		hm.RecordFailure(ep)
	}

	if !ep.IsDisabled() {
		t.Error("endpoint should be disabled after 3 failures")
	}
}

func TestHealthManagerRecordFailureBelowThreshold(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     5,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	ep := NewEndpointState("endpoint-a", "https://api.anthropic.com", "key", "")
	hm.AddEndpoint(ep)

	hm.RecordFailure(ep)
	hm.RecordFailure(ep)

	if ep.IsDisabled() {
		t.Error("endpoint should not be disabled with only 2 failures (threshold 5)")
	}
}

func TestHealthManagerRecordSuccessEnablesEndpoint(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     3,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	ep := NewEndpointState("endpoint-a", "https://api.anthropic.com", "key", "")
	hm.AddEndpoint(ep)

	ep.Disable()

	hm.RecordSuccess(ep)
	hm.RecordSuccess(ep)

	if ep.IsDisabled() {
		t.Error("endpoint should be re-enabled after 2 successes")
	}
}

func TestHealthManagerGetHealthyEndpoints(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     3,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	epA := NewEndpointState("endpoint-a", "https://api.anthropic.com", "key", "")
	epB := NewEndpointState("endpoint-b", "https://custom.com", "key", "")
	epC := NewEndpointState("endpoint-c", "https://another.com", "key", "")

	hm.AddEndpoint(epA)
	hm.AddEndpoint(epB)
	hm.AddEndpoint(epC)

	epB.Disable()

	healthy := hm.GetHealthyEndpoints()
	if len(healthy) != 2 {
		t.Errorf("healthy endpoints count = %d, want 2", len(healthy))
	}

	foundA, foundC := false, false
	for _, ep := range healthy {
		if ep.Name == "endpoint-a" {
			foundA = true
		}
		if ep.Name == "endpoint-c" {
			foundC = true
		}
	}
	if !foundA || !foundC {
		t.Error("endpoint-a and endpoint-c should be in healthy list")
	}
}

func TestHealthManagerIsEndpointHealthy(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     3,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	ep := NewEndpointState("endpoint-a", "https://api.anthropic.com", "key", "")
	hm.AddEndpoint(ep)

	if !hm.IsEndpointHealthy(ep) {
		t.Error("endpoint should initially be healthy")
	}

	ep.Disable()
	if hm.IsEndpointHealthy(ep) {
		t.Error("disabled endpoint should not be healthy")
	}
}

func TestProbeDisabledEndpointsSetsProbeFields(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     1,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	// Create a test server that returns 200 (healthy)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := NewEndpointState("test-ep", server.URL, "key", "")
	hm.AddEndpoint(ep)

	// Disable the endpoint
	ep.Disable()

	// Run probe on disabled endpoints
	hm.probeDisabledEndpoints()

	// Check that probe fields were set
	if ep.GetLastProbeTime().IsZero() {
		t.Error("lastProbeTime should be set after probe runs")
	}
	if !ep.GetLastProbeSuccess() {
		t.Error("lastProbeSuccess should be true when server returns 200")
	}
}

func TestProbeDisabledEndpointsRecordsFailure(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     1,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	// Create a test server that returns 500 (unhealthy)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ep := NewEndpointState("test-ep", server.URL, "key", "")
	hm.AddEndpoint(ep)

	// Disable the endpoint
	ep.Disable()

	// Run probe on disabled endpoints
	hm.probeDisabledEndpoints()

	// Check that probe fields were set
	if ep.GetLastProbeTime().IsZero() {
		t.Error("lastProbeTime should be set after probe runs")
	}
	if ep.GetLastProbeSuccess() {
		t.Error("lastProbeSuccess should be false when server returns 500")
	}
}

func TestProbeDisabledEndpointsRecordsRateLimitAsUnhealthy(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     1,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	// Create a test server that returns 429 (rate limited)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ep := NewEndpointState("test-ep", server.URL, "key", "")
	hm.AddEndpoint(ep)

	// Disable the endpoint
	ep.Disable()

	// Run probe on disabled endpoints
	hm.probeDisabledEndpoints()

	// Check that probe recorded 429 as unhealthy
	if ep.GetLastProbeTime().IsZero() {
		t.Error("lastProbeTime should be set after probe runs")
	}
	if ep.GetLastProbeSuccess() {
		t.Error("lastProbeSuccess should be false when server returns 429")
	}
}


func TestProbeDisabledEndpointsUsesProbeModel(t *testing.T) {
	hm := NewHealthManager(HealthConfig{
		FailuresToDisable:     1,
		RecoveryProbeInterval: 30 * time.Second,
		SuccessesToEnable:     2,
	})

	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &req)
		receivedModel = req.Model
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := NewEndpointState("test-ep", server.URL, "key", "")
	ep.ProbeModel = "my-backend-model"
	hm.AddEndpoint(ep)
	ep.Disable()


	hm.probeDisabledEndpoints()

	if receivedModel != "my-backend-model" {
		t.Errorf("probe sent model = %q, want %q", receivedModel, "my-backend-model")
	}
}
