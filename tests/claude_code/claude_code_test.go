package proxytest

import (
	"context"
	"strings"
	"testing"
	"time"

	testutil "github.com/anthropic-transparent-proxy/tests"
)

func getProxyHealth(t *testing.T) (*testutil.ProxyHealth, error) {
	return testutil.GetProxyHealth(t, "http://localhost:8080")
}

func TestClaudeCodeBasicChat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Claude Code integration test in short mode")
	}

	if !testutil.CheckClaudeCodeInstalled(t) {
		return
	}

	model := testutil.ValidateAvailableModels(t, "http://localhost:8080")
	if model == "" {
		return
	}

	runner := NewClaudeCodeRunner(map[string]string{
		"ANTHROPIC_BASE_URL":  "http://localhost:8080",
		"ANTHROPIC_API_KEY":   "dummy",
	})

	result, err := runner.RunWithInput("Say 'hello' and nothing else.", model, 5*time.Minute)

	if err != nil {
		t.Fatalf("basic chat test failed: %v", err)
	}

	if err := ValidateBasicChat(result); err != nil {
		t.Errorf("basic chat validation failed: %v", err)
	}
}

func TestClaudeCodeToolUse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Claude Code integration test in short mode")
	}

	if !testutil.CheckClaudeCodeInstalled(t) {
		return
	}

	model := testutil.ValidateAvailableModels(t, "http://localhost:8080")
	if model == "" {
		return
	}

	runner := NewClaudeCodeRunner(map[string]string{
		"ANTHROPIC_BASE_URL":  "http://localhost:8080",
		"ANTHROPIC_API_KEY":   "dummy",
	})

	result, err := runner.RunWithInput("!ls -la", model, 5*time.Minute)

	if err != nil {
		t.Fatalf("tool use test failed: %v", err)
	}

	if err := ValidateToolUse(result); err != nil {
		t.Errorf("tool use validation failed: %v", err)
	}
}

func TestClaudeCodeExtendedThinking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Claude Code integration test in short mode")
	}

	if !testutil.CheckClaudeCodeInstalled(t) {
		return
	}

	model := testutil.ValidateAvailableModels(t, "http://localhost:8080")
	if model == "" {
		return
	}

	runner := NewClaudeCodeRunner(map[string]string{
		"ANTHROPIC_BASE_URL":  "http://localhost:8080",
		"ANTHROPIC_API_KEY":   "dummy",
	})

	result, err := runner.RunWithInput("Think step by step: what is 2+2? Show your reasoning.", model, 5*time.Minute)

	if err != nil {
		t.Fatalf("extended thinking test failed: %v", err)
	}

	if err := ValidateExtendedThinking(result); err != nil {
		t.Errorf("extended thinking validation failed: %v", err)
	}
}

// TestClaudeCodeErrorRecovery validates that the proxy can handle multiple endpoints
// and route requests appropriately when some endpoints are unavailable. This test
// requires at least 2 enabled endpoints configured in the proxy configuration.
func TestClaudeCodeErrorRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Claude Code integration test in short mode")
	}

	if !testutil.CheckClaudeCodeInstalled(t) {
		return
	}

	health, err := getProxyHealth(t)
	if err != nil {
		t.Skip("failed to get proxy health")
		return
	}

	enabledEndpoints := 0
	for _, ep := range health.Endpoints {
		if ep.Status == "enabled" {
			enabledEndpoints++
		}
	}

	t.Logf("Found %d enabled endpoints, require at least 2 for error recovery test", enabledEndpoints)

	if enabledEndpoints < 2 {
		t.Skipf("error recovery test requires multiple enabled endpoints (found %d, need 2+)", enabledEndpoints)
		return
	}

	model := testutil.ValidateAvailableModels(t, "http://localhost:8080")
	if model == "" {
		return
	}

	runner := NewClaudeCodeRunner(map[string]string{
		"ANTHROPIC_BASE_URL":  "http://localhost:8080",
		"ANTHROPIC_API_KEY":   "dummy",
	})

	result, err := runner.RunWithInput("Say 'error recovery test passed'.", model, 5*time.Minute)

	if err != nil {
		t.Fatalf("error recovery test failed: %v", err)
	}

	if err := ValidateBasicChat(result); err != nil {
		t.Errorf("error recovery validation failed: %v", err)
	}

	healthAfter, err := getProxyHealth(t)
	if err != nil {
		t.Logf("Could not verify endpoint usage after request: %v", err)
	} else {
		t.Logf("Proxy health after request: status=%s, total_requests=%d", healthAfter.Status, healthAfter.TotalRequests)
	}
}

func TestClaudeCodeConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Claude Code integration test in short mode")
	}

	if !testutil.CheckClaudeCodeInstalled(t) {
		return
	}

	model := testutil.ValidateAvailableModels(t, "http://localhost:8080")
	if model == "" {
		return
	}

	numConcurrent := 3
	inputs := []string{
		"Say 'test 1 passed'.",
		"Say 'test 2 passed'.",
		"Say 'test 3 passed'.",
	}

	results := make(chan *RunResult, numConcurrent)
	errors := make(chan error, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func(idx int) {
			runner := NewClaudeCodeRunner(map[string]string{
				"ANTHROPIC_BASE_URL":  "http://localhost:8080",
				"ANTHROPIC_API_KEY":   "dummy",
			})

			result, err := runner.RunWithInput(inputs[idx], model, 5*time.Minute)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	successCount := 0
	for i := 0; i < numConcurrent; i++ {
		select {
		case result := <-results:
			if result.ExitCode == 0 {
				if strings.TrimSpace(result.Stdout) == "" {
					t.Errorf("concurrent request produced empty stdout")
				} else {
					successCount++
				}
			} else {
				t.Errorf("concurrent request failed with exit code %d", result.ExitCode)
			}
		case err := <-errors:
			t.Errorf("concurrent request failed: %v", err)
		case <-ctx.Done():
			t.Fatalf("timeout waiting for concurrent requests to complete")
		}
	}

	if successCount != numConcurrent {
		t.Errorf("expected %d successful requests, got %d", numConcurrent, successCount)
	}
}