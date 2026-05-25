package endpoint

import (
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
	ID string `json:"id"`
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

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

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
			models, err := DiscoverSupportedModels(ep)
			if err != nil {
				slog.Warn("model discovery failed", "endpoint", ep.Name, "error", err)
				ep.SetDiscoveryError(err.Error())
				if ep.ProbeModel == "test" && strings.Contains(ep.URL, "deepseek.com") {
					ep.ProbeModel = "deepseek-v4-flash"
				}
				return
			}
			ep.SetSupportedModels(models)
			ep.SetDiscoveryError("") // clear any previous error
			if len(models) > 0 && ep.ProbeModel == "test" {
				ep.ProbeModel = models[0]
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
