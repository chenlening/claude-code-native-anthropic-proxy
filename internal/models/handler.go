package models

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
)

// Handler handles /v1/models requests for model discovery
type Handler struct {
	healthMgr *endpoint.HealthManager
	logger    *slog.Logger
}

// NewHandler creates a new models handler
func NewHandler(hm *endpoint.HealthManager, logger *slog.Logger) *Handler {
	return &Handler{
		healthMgr: hm,
		logger:    logger,
	}
}

// ModelsResponse represents Anthropic API models endpoint response
type ModelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// ModelInfo represents a single model in the models response
type ModelInfo struct {
	ID string `json:"id"`
}

// ServeHTTP handles GET requests to /v1/models
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all endpoints
	allEndpoints := h.healthMgr.GetAllEndpoints()

	// Filter for healthy endpoints and collect unique model IDs
	modelSet := make(map[string]struct{})
	for _, ep := range allEndpoints {
		if !ep.IsDisabled() {
			for _, model := range ep.GetSupportedModels() {
				modelSet[model] = struct{}{}
			}
		}
	}

	// Convert to sorted slice
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)

	// Build response data
	modelInfos := make([]ModelInfo, len(models))
	for i, model := range models {
		modelInfos[i] = ModelInfo{ID: model}
	}

	response := ModelsResponse{Data: modelInfos}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	if anthropicVersion := r.Header.Get("Anthropic-Version"); anthropicVersion != "" {
		w.Header().Set("Anthropic-Version", anthropicVersion)
	}

	// Return JSON response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode models response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
