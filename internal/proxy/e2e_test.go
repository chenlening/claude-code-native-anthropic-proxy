package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/metrics"
)

// TestStartupVerificationE2E simulates the startup verification logic
func TestStartupVerificationE2E(t *testing.T) {
	// Create mock server that succeeds
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	ep := endpoint.NewEndpointState("test", mockServer.URL, "key", "")

	// Verification should succeed
	if err := ep.Verify("test"); err != nil {
		t.Errorf("unexpected error on verification: %v", err)
	}

	// Verify endpoint was not disabled
	if ep.IsDisabled() {
		t.Error("endpoint should not be disabled after successful verification")
	}
}

// TestProxyStartupWithFailingEndpoint tests startup with a failing endpoint
func TestProxyStartupWithFailingEndpoint(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})
	m := metrics.NewMetrics("test_startup")

	// Register endpoint that will fail verification
	ep := endpoint.NewEndpointState("bad", "http://localhost:99999/nonexistent", "key", "")
	hm.AddEndpoint(ep)

	// Simulate startup verification
	for _, e := range hm.GetAllEndpoints() {
		if err := e.Verify("test"); err != nil {
			t.Logf("expected failure for bad endpoint: %v", err)
			e.Disable()
			m.SetEndpointEnabled(e.Name, false)
		}
	}

	// Verify endpoint was disabled
	if !ep.IsDisabled() {
		t.Error("endpoint should be disabled after failed verification")
	}
}

// TestTimeoutHandling tests that verification respects timeout
func TestTimeoutHandling(t *testing.T) {
	// Create a slow server
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	ep := endpoint.NewEndpointState("slow", slowServer.URL, "key", "")
	ep.WithTimeout(50 * time.Millisecond)

	// Verification should timeout
	if err := ep.Verify("test"); err == nil {
		t.Error("expected timeout error, got nil")
	} else {
		t.Logf("expected timeout occurred: %v", err)
	}
}

// TestClosingReader tests the closingReader utility
func TestClosingReader(t *testing.T) {
	data := []byte("hello world")
	reader := &closingReader{src: data}

	buf := make([]byte, 5)
	n, _ := reader.Read(buf)
	if n != 5 || string(buf) != "hello" {
		t.Errorf("first read = %q, want hello", string(buf[:n]))
	}

	rest, _ := io.ReadAll(reader)
	if string(rest) != " world" {
		t.Errorf("remaining = %q, want ' world'", string(rest))
	}

	if err := reader.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestHealthManagerModelDiscovery tests that model discovery populates the model support map
func TestHealthManagerModelDiscovery(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})

	ep1 := endpoint.NewEndpointState("ep1", "http://localhost:1", "key", "")
	ep2 := endpoint.NewEndpointState("ep2", "http://localhost:2", "key", "")

	ep1.SetSupportedModels([]string{"model-a", "model-b"})
	ep2.SetSupportedModels([]string{"model-b", "model-c"})

	hm.AddEndpoint(ep1)
	hm.AddEndpoint(ep2)

	// Before DiscoverModelsOnce, modelSupport is empty
	if len(hm.GetEndpointsForModel("model-a")) != 0 {
		t.Error("expected no endpoints before DiscoverModelsOnce")
	}

	// After DiscoverModelsOnce, modelSupport should be populated
	hm.DiscoverModelsOnce()

	// Check model-a only ep1
	epA := hm.GetEndpointsForModel("model-a")
	if len(epA) != 1 || epA[0] != "ep1" {
		t.Errorf("model-a endpoints = %v, want [ep1]", epA)
	}

	// Check model-b has both ep1 and ep2
	epB := hm.GetEndpointsForModel("model-b")
	if len(epB) != 2 {
		t.Errorf("model-b endpoints count = %d, want 2", len(epB))
	}

	// Check model-c only ep2
	epC := hm.GetEndpointsForModel("model-c")
	if len(epC) != 1 || epC[0] != "ep2" {
		t.Errorf("model-c endpoints = %v, want [ep2]", epC)
	}
}

// TestSupportedModels tests the endpoint supported models functionality
func TestSupportedModels(t *testing.T) {
	ep := endpoint.NewEndpointState("test", "http://localhost", "key", "")

	// Initially empty
	models := ep.GetSupportedModels()
	if len(models) != 0 {
		t.Errorf("initial models count = %d, want 0", len(models))
	}

	// Set models
	ep.SetSupportedModels([]string{"model-x", "model-y"})
	models = ep.GetSupportedModels()
	if len(models) != 2 {
		t.Errorf("after set models count = %d, want 2", len(models))
	}

	// Check SupportsModel
	if !ep.SupportsModel("model-x") {
		t.Error("expected model-x to be supported")
	}
	if !ep.SupportsModel("model-y") {
		t.Error("expected model-y to be supported")
	}
	if ep.SupportsModel("model-z") {
		t.Error("model-z should not be supported")
	}
}