package endpoint

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverSupportedModels_WithModelsEndpoint(t *testing.T) {
	// Mock server that returns models list
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request uses the models_endpoint URL, not URL + /v1/models
		if r.URL.Path != "/models" {
			t.Errorf("expected path /models, got %s", r.URL.Path)
		}

		// Verify Anthropic-Version header is NOT set for non-/v1/ path
		if r.Header.Get("Anthropic-Version") != "" {
			t.Error("Anthropic-Version header should not be set for /models path")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		response := ModelsResponse{
			Data: []ModelInfo{
				{ID: "deepseek-chat"},
				{ID: "deepseek-coder"},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create endpoint with custom models_endpoint
	ep := NewEndpointState("test", "https://api.deepseek.com/anthropic", "test-key", server.URL+"/models")
	models, err := DiscoverSupportedModels(ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify we got the expected models
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0] != "deepseek-chat" {
		t.Errorf("expected first model deepseek-chat, got %s", models[0])
	}
}

func TestDiscoverSupportedModels_WithoutModelsEndpoint(t *testing.T) {
	// Mock server that returns models list at /v1/models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request uses default URL + /v1/models
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected path /v1/models, got %s", r.URL.Path)
		}

		// Verify Anthropic-Version header IS set for /v1/ path
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Error("Anthropic-Version header should be set for /v1/models path")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		response := ModelsResponse{
			Data: []ModelInfo{
				{ID: "claude-opus-4-20250514"},
				{ID: "claude-sonnet-4-20250514"},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create endpoint without custom models_endpoint
	ep := NewEndpointState("test", server.URL, "test-key", "")
	models, err := DiscoverSupportedModels(ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify we got the expected models
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}