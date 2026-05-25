package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

type ProxyHealth struct {
	Status           string                    `json:"status"`
	TotalRequests    int64                     `json:"total_requests"`
	Endpoints        map[string]EndpointHealth `json:"endpoints"`
	Models           map[string]interface{}    `json:"models"`
}

type EndpointHealth struct {
	Status              string   `json:"status"`
	Requests            int64    `json:"requests"`
	Failures            int64    `json:"failures"`
	ActiveConnections   int      `json:"active_connections"`
	SupportedModels     []string `json:"supported_models"`
}

func GetProxyHealth(t *testing.T, proxyURL string) (*ProxyHealth, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(proxyURL + "/health")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	var health ProxyHealth
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to parse health response: %w", err)
	}

	return &health, nil
}

type ModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func ValidateAvailableModels(t *testing.T, proxyURL string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(proxyURL + "/v1/models")
	if err != nil {
		t.Skipf("failed to connect to proxy: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("proxy returned status %d", resp.StatusCode)
		return ""
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		t.Skipf("failed to parse models response: %v", err)
		return ""
	}

	if len(modelsResp.Data) == 0 {
		t.Skip("no models available")
		return ""
	}

	t.Logf("Using model: %s", modelsResp.Data[0].ID)
	return modelsResp.Data[0].ID
}

func CheckClaudeCodeInstalled(t *testing.T) bool {
	cmd := exec.Command("which", "claude")
	if err := cmd.Run(); err != nil {
		t.Skip("Claude Code not installed, skipping integration tests")
		return false
	}
	return true
}