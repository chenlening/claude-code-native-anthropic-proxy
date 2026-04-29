package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropic-transparent-proxy/internal/config"
	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/metrics"
	"github.com/anthropic-transparent-proxy/internal/routing"
	"log/slog"
)

// TestProxyE2E tests the proxy with mock backends
func TestProxyE2E(t *testing.T) {
	// Create mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") == "" {
			t.Error("Authorization header missing")
		}
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Errorf("Anthropic-Version = %q, want 2023-06-01", r.Header.Get("Anthropic-Version"))
		}

		// Read and parse request
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// Verify model mapping (skip for "test" model used in Verify())
		model, _ := req["model"].(string)
		if model == "custom-sonnet" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      "msg_test123",
				"type":    "message",
				"model":   "custom-sonnet",
				"content": []map[string]interface{}{{"type": "text", "text": "Hello from mock!"}},
			})
			return
		}

		// For Verify() requests (model="test"), return success
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Setup config
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "mock", Model: "custom-sonnet", Weight: 10},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"mock": {URL: upstream.URL, APIKey: "test-key"},
		},
	}

	// Initialize components
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	ep := endpoint.NewEndpointState("mock", upstream.URL, "test-key")
	hm.AddEndpoint(ep)

	// Verify endpoint works
	if err := ep.Verify("test"); err != nil {
		t.Fatalf("endpoint verification failed: %v", err)
	}

	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	m := metrics.NewMetrics("test_e2e")
	logger := slog.Default()

	handler := NewHandler(router, hm, m, logger)

	// Make request
	reqBody := `{"model": "claude-sonnet-4", "max_tokens": 10, "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "user-key")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp["id"] != "msg_test123" {
		t.Errorf("id = %q, want msg_test123", resp["id"])
	}
	if resp["type"] != "message" {
		t.Errorf("type = %q, want message", resp["type"])
	}
}

// TestEndpointVerification tests the endpoint verification method
func TestEndpointVerification(t *testing.T) {
	// Test unreachable endpoint
	ep := endpoint.NewEndpointState("unreachable", "http://localhost:99999/nonexistent", "key")
	if err := ep.Verify("test"); err == nil {
		t.Error("expected error for unreachable endpoint, got nil")
	}

	// Test with mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	ep2 := endpoint.NewEndpointState("mock", mockServer.URL, "test-key")
	if err := ep2.Verify("test"); err != nil {
		t.Errorf("unexpected error for mock server: %v", err)
	}
}

// TestEndpointVerificationWithRealBackends is an integration test against real backends
// Skipped if API keys are not set or proxy is not running
func TestEndpointVerificationWithRealBackends(t *testing.T) {
	gzlKey := os.Getenv("GZL_API_KEY")
	aliyunKey := os.Getenv("ALIYUN_API_KEY")

	if gzlKey == "" || aliyunKey == "" {
		t.Skip("GZL_API_KEY or ALIYUN_API_KEY not set, skipping real backend test")
	}

	// Check proxy is running
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		t.Skip("proxy not running at localhost:8080, skipping real backend test")
	}
	resp.Body.Close()

	// Load config to get model names
	cfg, err := config.Load("../../configs/proxy.yaml")
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Find backend model for each endpoint from config
	gzlModel := ""
	aliyunModel := ""
	for _, modelCfg := range cfg.Models {
		for _, backend := range modelCfg.Backends {
			if backend.Endpoint == "gzl" && gzlModel == "" {
				gzlModel = backend.Model
			}
			if backend.Endpoint == "aliyun" && aliyunModel == "" {
				aliyunModel = backend.Model
			}
		}
	}

	// Test GZL endpoint
	gzlEp := endpoint.NewEndpointState("gzl", "https://open.bigmodel.cn/api/anthropic", gzlKey)
	if err := gzlEp.Verify(gzlModel); err != nil {
		t.Errorf("GZL endpoint verification failed: %v", err)
	} else {
		t.Logf("GZL endpoint verified successfully with model %q", gzlModel)
	}

	// Test Aliyun endpoint
	aliyunEp := endpoint.NewEndpointState("aliyun", "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic", aliyunKey)
	if err := aliyunEp.Verify(aliyunModel); err != nil {
		t.Errorf("Aliyun endpoint verification failed: %v", err)
	} else {
		t.Logf("Aliyun endpoint verified successfully with model %q", aliyunModel)
	}
}

// TestStartupVerificationE2E simulates the startup verification logic
func TestStartupVerificationE2E(t *testing.T) {
	// Create mock server that succeeds
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	ep := endpoint.NewEndpointState("test", mockServer.URL, "key")

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
	ep := endpoint.NewEndpointState("bad", "http://localhost:99999/nonexistent", "key")
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

// TestProxyRequestAfterStartupVerification tests that proxy works after startup verification
func TestProxyRequestAfterStartupVerification(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// For Verify() requests (model="test"), return success
		if req["model"] == "test" {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "msg_after_verify",
			"type":    "message",
			"model":   "test-model",
			"content": []map[string]interface{}{{"type": "text", "text": "Works!"}},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "upstream", Model: "test-model"},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"upstream": {URL: upstream.URL, APIKey: "key"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	ep := endpoint.NewEndpointState("upstream", upstream.URL, "key")
	hm.AddEndpoint(ep)
	m := metrics.NewMetrics("test_after_verify")

	// Verify at startup
	if err := ep.Verify("test"); err != nil {
		t.Fatalf("startup verification failed: %v", err)
	}

	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	logger := slog.Default()
	handler := NewHandler(router, hm, m, logger)

	// Make request
	reqBody := `{"model": "claude-sonnet-4", "max_tokens": 10, "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp["id"] != "msg_after_verify" {
		t.Errorf("id = %q, want msg_after_verify", resp["id"])
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

	ep := endpoint.NewEndpointState("slow", slowServer.URL, "key")
	ep.WithTimeout(50 * time.Millisecond)

	// Verification should timeout
	if err := ep.Verify("test"); err == nil {
		t.Error("expected timeout error, got nil")
	} else {
		t.Logf("expected timeout occurred: %v", err)
	}
}
