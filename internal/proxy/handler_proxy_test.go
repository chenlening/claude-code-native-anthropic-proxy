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

func TestProxyHandlerForwardsRequest(t *testing.T) {
	// Create upstream mock server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the model was replaced
		body, _ := io.ReadAll(r.Body)

		var req map[string]interface{}
		json.Unmarshal(body, &req)

		if req["model"] != "custom-sonnet" {
			t.Errorf("upstream received model = %v, want custom-sonnet", req["model"])
		}

		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("upstream received Authorization = %q, want Bearer test-api-key", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_test",
			"model": "custom-sonnet",
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello!"},
			},
		})
	}))
	defer upstream.Close()

	// Setup routing
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "upstream", Model: "custom-sonnet", Weight: 10},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"upstream": {URL: upstream.URL, APIKey: "test-api-key"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	ep := endpoint.NewEndpointState("upstream", upstream.URL, "test-api-key")
	hm.AddEndpoint(ep)

	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	m := metrics.NewMetrics("test_proxy")
	logger := slog.Default()

	handler := NewHandler(router, hm, m, logger)

	// Send request
	reqBody := `{"model": "claude-sonnet-4", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "original-key")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["id"] != "msg_test" {
		t.Errorf("response id = %v, want msg_test", resp["id"])
	}
}

func TestProxyHandlerUnknownModel(t *testing.T) {
	cfg := &config.Config{
		Models:    map[string]config.ModelConfig{},
		Endpoints: map[string]config.EndpointConfig{},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})
	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	m := metrics.NewMetrics("test_proxy")
	logger := slog.Default()

	handler := NewHandler(router, hm, m, logger)

	reqBody := `{"model": "unknown-model", "messages": []}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want 503", rec.Code)
	}
}

func TestProxyHandlerInvalidBody(t *testing.T) {
	cfg := &config.Config{
		Models:    map[string]config.ModelConfig{},
		Endpoints: map[string]config.EndpointConfig{},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})
	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	m := metrics.NewMetrics("test_proxy")
	logger := slog.Default()

	handler := NewHandler(router, hm, m, logger)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400", rec.Code)
	}
}

func TestProxyHandlerSSEStreaming(t *testing.T) {
	// Create upstream that returns SSE
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		flusher := w.(http.Flusher)
		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hi\"}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, event := range events {
			w.Write([]byte(event))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "upstream", Model: "claude-sonnet-4"},
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

	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	m := metrics.NewMetrics("test_proxy")
	logger := slog.Default()

	handler := NewHandler(router, hm, m, logger)

	reqBody := `{"model": "claude-sonnet-4", "stream": true, "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "message_start") {
		t.Error("SSE response should contain message_start event")
	}
	if !strings.Contains(body, "content_block_delta") {
		t.Error("SSE response should contain content_block_delta event")
	}
	if !strings.Contains(body, "message_stop") {
		t.Error("SSE response should contain message_stop event")
	}
}

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

func TestRetryOn429(t *testing.T) {
	// First backend returns 429, second returns 200 with SSE
	callCount := 0
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer firstUpstream.Close()

	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, event := range events {
			w.Write([]byte(event))
			flusher.Flush()
		}
	}))
	defer secondUpstream.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "first", Model: "custom-sonnet", Weight: 10},
					{Endpoint: "second", Model: "custom-sonnet", Weight: 10},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"first":  {URL: firstUpstream.URL, APIKey: "key1"},
			"second": {URL: secondUpstream.URL, APIKey: "key2"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	ep1 := endpoint.NewEndpointState("first", firstUpstream.URL, "key1")
	ep2 := endpoint.NewEndpointState("second", secondUpstream.URL, "key2")
	hm.AddEndpoint(ep1)
	hm.AddEndpoint(ep2)

	router := routing.NewModelRouter(cfg, hm, routing.NewLeastConnectionsLoadBalancer())
	m := metrics.NewMetrics("test_proxy")
	logger := slog.Default()

	handler := NewHandler(router, hm, m, logger)

	reqBody := `{"model": "claude-sonnet-4", "stream": true, "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200 (should retry on 429)", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "message_start") {
		t.Error("response should contain message_start event from second backend")
	}

	if callCount != 2 {
		t.Errorf("expected 2 backend calls (429 + 200), got %d", callCount)
	}
}

// Integration tests against running proxy - skipped if proxy not available
func TestIntegrationProxyHealth(t *testing.T) {
	// Check if proxy is running and responding with health JSON
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		t.Skip("proxy not running at localhost:8080, skipping integration test")
	}
	defer resp.Body.Close()

	// Verify it's actually our proxy by checking response body is JSON
	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Skip("localhost:8080 is not our proxy, skipping integration test")
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check status = %d, want 200", resp.StatusCode)
	}

	if health["status"] != "healthy" && health["status"] != "degraded" {
		t.Errorf("health status = %v, want healthy or degraded", health["status"])
	}
}

func TestIntegrationProxyToGZL(t *testing.T) {
	gzlKey := os.Getenv("GZL_API_KEY")
	if gzlKey == "" {
		t.Skip("GZL_API_KEY not set, skipping integration test")
	}

	// Check proxy is running first
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		t.Skip("proxy not running at localhost:8080, skipping integration test")
	}
	resp.Body.Close()

	// Send request to proxy
	reqBody := `{"model": "claude-sonnet-4-20250514", "max_tokens": 10, "messages": [{"role": "user", "content": "Hi"}]}`
	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", gzlKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200. body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["type"] != "message" {
		t.Errorf("response type = %v, want message", result["type"])
	}
}

func TestIntegrationProxyToAliyun(t *testing.T) {
	aliyunKey := os.Getenv("ALIYUN_API_KEY")
	if aliyunKey == "" {
		t.Skip("ALIYUN_API_KEY not set, skipping integration test")
	}

	// Check proxy is running first
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		t.Skip("proxy not running at localhost:8080, skipping integration test")
	}
	resp.Body.Close()

	// Send request to proxy
	reqBody := `{"model": "claude-haiku-3-5-20241022", "max_tokens": 10, "messages": [{"role": "user", "content": "Hi"}]}`
	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", aliyunKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200. body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["type"] != "message" {
		t.Errorf("response type = %v, want message", result["type"])
	}
}
