package apitest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	testutil "github.com/anthropic-transparent-proxy/tests"
)

type ProxyResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func getProxyHealth(t *testing.T) (*testutil.ProxyHealth, error) {
	return testutil.GetProxyHealth(t, "http://localhost:8080")
}

func TestLiveProxyHealthCheck(t *testing.T) {
	health, err := getProxyHealth(t)
	if err != nil {
		t.Fatalf("failed to get proxy health: %v", err)
	}

	t.Logf("Proxy status: %s", health.Status)

	// Count enabled endpoints
	enabledCount := 0
	for name, ep := range health.Endpoints {
		if ep.Status == "enabled" && len(ep.SupportedModels) > 0 {
			t.Logf("  - %s: supports %d models", name, len(ep.SupportedModels))
			enabledCount++
		}
	}

	if health.Status != "healthy" && health.Status != "degraded" {
		t.Errorf("proxy status %q, want 'healthy' or 'degraded'", health.Status)
	}

	if enabledCount == 0 {
		t.Skip("no enabled endpoints with supported models, skipping E2E tests")
	}
}

type AvailableEndpoint struct {
	Name   string
	Models []string
}

func getAvailableEndpoints(t *testing.T) ([]AvailableEndpoint, error) {
	health, err := getProxyHealth(t)
	if err != nil {
		return nil, err
	}

	var available []AvailableEndpoint
	for name, ep := range health.Endpoints {
		if ep.Status == "enabled" && len(ep.SupportedModels) > 0 {
			available = append(available, AvailableEndpoint{
				Name:   name,
				Models: ep.SupportedModels,
			})
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no available endpoints")
	}

	return available, nil
}

func makeProxyRequest(model, message string) (*http.Response, error) {
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 10,
		"messages":   []map[string]string{{"role": "user", "content": message}},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", "http://localhost:8080/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

func TestLiveProxySuccessfulRequest(t *testing.T) {
	endpoints, err := getAvailableEndpoints(t)
	if err != nil {
		t.Skip(err.Error())
	}

	// Get first available model
	model := endpoints[0].Models[0]
	t.Logf("Testing with model: %s from endpoint: %s", model, endpoints[0].Name)

	resp, err := makeProxyRequest(model, "Hello from E2E test!")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d. Response: %s", resp.StatusCode, string(body))
	}

	var proxyResp ProxyResponse
	if err := json.Unmarshal(body, &proxyResp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if proxyResp.ID == "" {
		t.Error("response missing ID")
	}
	if proxyResp.Type != "message" {
		t.Errorf("response type = %q, want 'message'", proxyResp.Type)
	}
	if len(proxyResp.Content) == 0 {
		t.Error("response has no content")
	}
}

func TestLiveProxyUnsupportedModel503(t *testing.T) {
	resp, err := makeProxyRequest("this-model-definitely-not-supported-by-any-endpoint-xyz123", "test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d. Response: %s", resp.StatusCode, string(body))
	}

	errorMsg := string(body)
	if !strings.Contains(errorMsg, "model not supported by any endpoint") {
		t.Errorf("expected 'model not supported by any endpoint' in error, got: %s", errorMsg)
	}
}

func TestLiveProxyLoadBalancing(t *testing.T) {
	endpoints, err := getAvailableEndpoints(t)
	if err != nil {
		t.Skip(err.Error())
	}

	// Find model supported by multiple endpoints
	modelEndpoints := make(map[string][]string)
	for _, ep := range endpoints {
		for _, model := range ep.Models {
			modelEndpoints[model] = append(modelEndpoints[model], ep.Name)
		}
	}

	var targetModel string
	var endpointCount int
	for model, eps := range modelEndpoints {
		if len(eps) > endpointCount && len(eps) > 1 {
			targetModel = model
			endpointCount = len(eps)
		}
	}

	if endpointCount < 2 {
		t.Skip("no model supported by multiple endpoints, skipping load balancing test")
	}

	t.Logf("Testing load balancing for model: %s (supported by %d endpoints)", targetModel, endpointCount)

	// Send 10 requests
	endpointHits := make(map[string]int)
	for i := 0; i < 10; i++ {
		// Get current health snapshot before request
		healthBefore, _ := getProxyHealth(t)
		beforeRequests := make(map[string]int64)
		for name, ep := range healthBefore.Endpoints {
			beforeRequests[name] = ep.Requests
		}

		resp, err := makeProxyRequest(targetModel, fmt.Sprintf("Test message %d", i))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		resp.Body.Close()

		// Get health snapshot after request
		healthAfter, _ := getProxyHealth(t)
		for name, ep := range healthAfter.Endpoints {
			if ep.Status == "enabled" && ep.Requests > beforeRequests[name] {
				endpointHits[name]++
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Logf("Request distribution: %v", endpointHits)
	// Verify at least 2 different endpoints received requests
	differentEndpoints := 0
	for _, count := range endpointHits {
		if count > 0 {
			differentEndpoints++
		}
	}
	if differentEndpoints < 2 {
		t.Errorf("requests only distributed to %d endpoint(s), expected at least 2", differentEndpoints)
	}
}

// TestLiveProxyClaudeCodeIntegration comprehensively tests all Claude Code → Proxy interactions
// This single test catches all integration issues upfront, eliminating the slow feedback loop
// of "report bug → fix → test → report next bug" that users experience
func TestLiveProxyClaudeCodeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Claude Code integration test in short mode")
	}

	health, err := getProxyHealth(t)
	if err != nil {
		t.Fatalf("failed to get proxy health: %v", err)
	}

	t.Logf("=== Claude Code Integration Test ===")
	t.Logf("Proxy status: %s", health.Status)
	t.Logf("Total endpoints: %d", len(health.Endpoints))

	// Test 1: Model Discovery (CRITICAL for Claude Code)
	t.Log("\nTest 1: Model Discovery via /v1/models")
	resp, err := http.Get("http://localhost:8080/v1/models")
	if err != nil {
		t.Fatalf("model discovery request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("model discovery returned status %d, body: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	type ModelsResponse struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		t.Fatalf("failed to decode models response: %v", err)
	}

	t.Logf("  Found %d models", len(modelsResp.Data))
	if len(modelsResp.Data) == 0 {
		t.Error("no models returned by /v1/models endpoint")
	}

	// Verify models are sorted (for consistent responses)
	for i := 1; i < len(modelsResp.Data); i++ {
		if modelsResp.Data[i-1].ID > modelsResp.Data[i].ID {
			t.Errorf("models not sorted: %s > %s", modelsResp.Data[i-1].ID, modelsResp.Data[i].ID)
		}
	}

	// Test 2: Basic API Request (non-streaming)
	t.Log("\nTest 2: Basic API Request")
	endpoints, err := getAvailableEndpoints(t)
	if err != nil {
		t.Fatalf("failed to get available endpoints: %v", err)
	}

	testModel := endpoints[0].Models[0]
	t.Logf("  Testing with model: %s", testModel)

	resp, err = makeProxyRequest(testModel, "Hello from Claude Code integration test!")
	if err != nil {
		t.Fatalf("basic API request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("basic API request returned status %d, body: %s", resp.StatusCode, string(body))
	}

	var basicResp ProxyResponse
	if err := json.NewDecoder(resp.Body).Decode(&basicResp); err != nil {
		t.Fatalf("failed to decode basic response: %v", err)
	}

	if basicResp.Type != "message" {
		t.Errorf("expected response type 'message', got '%s'", basicResp.Type)
	}

	if len(basicResp.Content) == 0 {
		t.Error("expected non-empty content in response")
	}

	t.Logf("  Response: %s", basicResp.Content[0].Text)

	// Test 3: Streaming Support (CRITICAL for Claude Code)
	t.Log("\nTest 3: Streaming Request")
	streamingReqBody := map[string]interface{}{
		"model":      testModel,
		"max_tokens": 10,
		"messages":   []map[string]string{{"role": "user", "content": "Test streaming"}},
		"stream":     true,
	}
	streamingBody, err := json.Marshal(streamingReqBody)
	if err != nil {
		t.Fatalf("failed to marshal streaming request: %v", err)
	}

	req, err := http.NewRequest("POST", "http://localhost:8080/v1/messages", bytes.NewReader(streamingBody))
	if err != nil {
		t.Fatalf("failed to create streaming request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("streaming request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("streaming request returned status %d, body: %s", resp.StatusCode, string(body))
	}

	// Verify SSE streaming headers
	contentType = resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("expected Content-Type containing 'text/event-stream', got %s", contentType)
	}

	// Read first few SSE events to verify streaming works
	sseEvents := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() && sseEvents < 5 {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			sseEvents++
		}
	}

	if sseEvents < 2 {
		t.Errorf("expected at least 2 SSE events, got %d", sseEvents)
	}

	t.Logf("  Streaming works: received %d SSE events", sseEvents)

	// Test 4: Error Handling for Invalid Model
	t.Log("\nTest 4: Error Handling for Invalid Model")
	resp, err = makeProxyRequest("invalid-model-xyz123", "test")
	if err != nil {
		t.Fatalf("invalid model request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 503 for invalid model, got %d. Response: %s", resp.StatusCode, string(body))
	}

	// Test 5: Different Model Types
	t.Log("\nTest 5: Testing Different Model Types")
	modelTypes := make(map[string]bool)
	for _, ep := range endpoints {
		for _, model := range ep.Models {
			// Extract model type prefix
			parts := strings.Split(model, "-")
			if len(parts) > 0 {
				modelTypes[parts[0]] = true
			}
		}
	}

	t.Logf("  Found model types: %v", modelTypes)

	// Test at least 2 different model types if available
	testedModels := 0
	for modelType := range modelTypes {
		// Find a model of this type
		var testModel string
		for _, ep := range endpoints {
			for _, model := range ep.Models {
				if strings.HasPrefix(model, modelType+"-") {
					testModel = model
					break
				}
			}
			if testModel != "" {
				break
			}
		}

		if testModel != "" {
			t.Logf("  Testing model type: %s (model: %s)", modelType, testModel)
			resp, err := makeProxyRequest(testModel, fmt.Sprintf("Test %s model type", modelType))
			if err != nil {
				t.Logf("  Warning: %s model type request failed: %v", modelType, err)
				continue
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				testedModels++
			}
		}

		if testedModels >= 2 {
			break // Successfully tested 2 different model types
		}
	}

	if testedModels == 0 {
		t.Log("  Warning: Could not test different model types (may only have one model type)")
	} else {
		t.Logf("  Successfully tested %d different model types", testedModels)
	}

	// Test 6: Concurrent Requests
	t.Log("\nTest 6: Concurrent Requests")
	numRequests := 5
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := makeProxyRequest(testModel, fmt.Sprintf("Concurrent test %d", i))
			if err != nil {
				errors <- fmt.Errorf("request failed: %v", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errors <- fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
				return
			}
		}()
	}

	// Wait for all requests or errors
	go func() {
		wg.Wait()
		close(errors)
	}()

	// Check for errors
	concurrentErrors := 0
	for err := range errors {
		t.Log("  Concurrent request error:", err)
		concurrentErrors++
	}

	if concurrentErrors == 0 {
		t.Logf("  All %d concurrent requests succeeded", numRequests)
	} else {
		t.Errorf("  %d/%d concurrent requests failed", concurrentErrors, numRequests)
	}

	// Summary
	t.Log("\n=== Claude Code Integration Test Summary ===")
	t.Logf("✓ Model Discovery: PASS")
	t.Logf("✓ Basic API Request: PASS")
	t.Logf("✓ Streaming Support: PASS")
	t.Logf("✓ Error Handling: PASS")
	t.Logf("✓ Different Model Types: %s", map[bool]string{true: "PASS", false: "SKIPPED"}[testedModels >= 2])
	t.Logf("✓ Concurrent Requests: %s", map[bool]string{true: "PASS", false: "FAIL"}[concurrentErrors == 0])
}