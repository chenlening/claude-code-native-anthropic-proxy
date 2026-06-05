package endpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ModelsResponse represents the GET /v1/models response
type ModelsResponse struct {
	Data []ModelInfo `json:"data"`
}

type ModelInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// DiscoverSupportedModels probes an endpoint's models endpoint and returns a list
// with 30 second timeout to prevent hanging on slow endpoints.
func DiscoverSupportedModels(ep *EndpointState) ([]string, error) {
	// Determine which URL to use
	discoveryURL := ep.ModelsEndpoint
	if discoveryURL == "" {
		discoveryURL = ep.URL + "/v1/models"
	}

	req, err := http.NewRequest("GET", discoveryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ep.APIKey)

	// Only set Anthropic-Version header for /v1/models paths
	if len(discoveryURL) >= 10 && discoveryURL[len(discoveryURL)-10:] == "/v1/models" {
		req.Header.Set("Anthropic-Version", "2023-06-01")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status=%d url=%s body=%s", resp.StatusCode, discoveryURL, string(body))
	}

	// Try to decode as standard ModelsResponse first
	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, err
	}

	// Check for non-standard error responses that return HTTP 200 (e.g. bigmodel.cn)
	if len(mr.Data) == 0 {
		var errResp struct {
			Code    int    `json:"code"`
			Msg     string `json:"msg"`
			Success *bool  `json:"success"`
		}
		if json.Unmarshal(body, &errResp) == nil {
			if errResp.Success != nil && !*errResp.Success {
				return nil, fmt.Errorf("api error (HTTP 200): code=%d msg=%s", errResp.Code, errResp.Msg)
			}
		}
	}

	seen := make(map[string]struct{})
	models := make([]string, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.Status == "Shutdown" || m.Status == "Retiring" {
			continue
		}
		if _, exists := seen[m.ID]; !exists {
			seen[m.ID] = struct{}{}
			models = append(models, m.ID)
		}
	}
	return models, nil
}

// StartModelDiscovery probes all registered endpoints and builds a model support map.
// Runs initial discovery then refreshes every discoveryInterval.
func (h *HealthManager) StartModelDiscovery(discoveryInterval time.Duration, stopCh <-chan struct{}) {
	h.discoverModels()
	ticker := time.NewTicker(discoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.discoverModels()
		case <-stopCh:
			return
		}
	}
}

func (h *HealthManager) discoverModels() {
	h.mu.Lock()
	defer h.mu.Unlock()

	var wg sync.WaitGroup
	for _, ep := range h.endpoints {
		wg.Add(1)
		go func(ep *EndpointState) {
			defer wg.Done()
			var models []string
			var err error
			if len(ep.ConfiguredModels) > 0 {
				models = validateConfiguredModels(ep)
				if len(models) == 0 {
					err = fmt.Errorf("all %d configured models failed probing", len(ep.ConfiguredModels))
				}
			} else {
				models, err = DiscoverSupportedModels(ep)
			}
			if err != nil {
				slog.Warn("model discovery failed", "endpoint", ep.Name, "error", err)
				ep.SetDiscoveryError(err.Error())
				if ep.ProbeModel == "test" {
					if strings.Contains(ep.URL, "deepseek.com") {
						ep.ProbeModel = "deepseek-v4-flash"
					} else if strings.Contains(ep.URL, "volces.com") {
						ep.ProbeModel = "deepseek-v4-pro-260425"
					}
				}
				return
			}
			ep.SetSupportedModels(models)
			ep.SetDiscoveryError("") // clear any previous error
			if len(models) > 0 && ep.ProbeModel == "test" {
				ep.ProbeModel = pickProbeModel(ep.URL, models)
			}
		}(ep)
	}
	wg.Wait()

	// Rebuild model → endpoints map
	h.modelSupport = make(map[string][]string)
	for _, ep := range h.endpoints {
		for _, model := range ep.GetSupportedModels() {
			h.modelSupport[model] = append(h.modelSupport[model], ep.Name)
		}
	}
}

// DiscoverModelsOnce performs a single synchronous discovery of all endpoint models.
// Returns immediately after discovery completes or times out.
func (h *HealthManager) DiscoverModelsOnce() {
	h.discoverModels()
}

// RunFailedModelReprobe periodically re-probes only the models that failed initial
// discovery. When a previously-failed model succeeds, it's added to supported models
// and the routing map is rebuilt. Runs until stopCh is closed.
func (h *HealthManager) RunFailedModelReprobe(interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.reprobeFailedModels()
		case <-stopCh:
			return
		}
	}
}

// reprobeFailedModels checks all endpoints for models that failed probing and
// re-probes only those. Successfully re-probed models are promoted to supported.
func (h *HealthManager) reprobeFailedModels() {
	h.mu.Lock()
	defer h.mu.Unlock()

	var changed bool
	var wg sync.WaitGroup

	for _, ep := range h.endpoints {
		failed := ep.GetFailedModels()
		if len(failed) == 0 {
			continue
		}
		wg.Add(1)
		go func(ep *EndpointState, failed []string) {
			defer wg.Done()
			for _, model := range failed {
				if probeModel(ep, model) {
					slog.Info("failed model recovered", "endpoint", ep.Name, "model", model)
					ep.AddSupportedModel(model)
					ep.RemoveFailedModel(model)
					changed = true
				}
			}
		}(ep, failed)
	}
	wg.Wait()

	if changed {
		// Rebuild model → endpoints map
		h.modelSupport = make(map[string][]string)
		for _, ep := range h.endpoints {
			for _, model := range ep.GetSupportedModels() {
				h.modelSupport[model] = append(h.modelSupport[model], ep.Name)
			}
		}
	}
}

// validateConfiguredModels probes each configured model against the endpoint and
// returns only the ones that succeed. Failed models are tracked for later re-probing.
func validateConfiguredModels(ep *EndpointState) []string {
	var mu sync.Mutex
	var valid []string
	var failed []string
	var wg sync.WaitGroup

	for _, model := range ep.ConfiguredModels {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			if probeModel(ep, model) {
				mu.Lock()
				valid = append(valid, model)
				mu.Unlock()
			} else {
				mu.Lock()
				failed = append(failed, model)
				mu.Unlock()
				slog.Warn("configured model not working, skipping", "endpoint", ep.Name, "model", model)
			}
		}(model)
	}
	wg.Wait()
	ep.SetFailedModels(failed)
	return valid
}

// probeModel sends a minimal request to the endpoint to check if a model works.
// Retries once on transport-level errors (timeout, connection reset) to avoid
// marking a working model as dead due to transient network issues.
func probeModel(ep *EndpointState, model string) bool {
	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
	}
	body, _ := json.Marshal(payload)

	const probeTimeout = 30 * time.Second
	const maxAttempts = 2

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequest("POST", ep.URL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+ep.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")

		client := &http.Client{Timeout: probeTimeout}
		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("model probe network error", "endpoint", ep.Name, "model", model, "attempt", attempt, "error", err)
			if attempt < maxAttempts {
				continue
			}
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			return true
		}

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		slog.Debug("model probe failed", "endpoint", ep.Name, "model", model, "attempt", attempt, "status", resp.StatusCode, "body", string(respBody))
		return false
	}
	return false
}

// pickProbeModel selects a suitable model for health probes.
// For Volces coding plan, not all models support the plan feature, so we
// prefer known-working deepseek models. Falls back to the first model.
func pickProbeModel(endpointURL string, models []string) string {
	if strings.Contains(endpointURL, "volces.com") {
		for _, m := range models {
			if strings.HasPrefix(m, "deepseek-v4") {
				return m
			}
		}
	}
	return models[0]
}
