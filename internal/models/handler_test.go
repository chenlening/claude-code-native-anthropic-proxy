package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
)

func TestModelsHandlerReturnsModels(t *testing.T) {
	// Create real health manager for testing
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{
		FailuresToDisable: 5,
		RecoveryProbeInterval: 30,
		SuccessesToEnable: 2,
	})

	// Create mock endpoint with models
	mockEp := endpoint.NewEndpointState("test-endpoint", "https://example.com/api", "test-key", "")
	mockEp.SetSupportedModels([]string{"model-1", "model-2", "model-3"})
	hm.AddEndpoint(mockEp)

	handler := NewHandler(hm, nil)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	// Call handler
	handler.ServeHTTP(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Parse response
	var response ModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check models returned
	if len(response.Data) != 3 {
		t.Errorf("expected 3 models, got %d", len(response.Data))
	}

	// Check models are sorted
	for i := 1; i < len(response.Data); i++ {
		if response.Data[i-1].ID > response.Data[i].ID {
			t.Errorf("models not sorted: %s > %s", response.Data[i-1].ID, response.Data[i].ID)
		}
	}
}

func TestModelsHandlerHandlesNoModels(t *testing.T) {
	// Create real health manager
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{
		FailuresToDisable: 5,
		RecoveryProbeInterval: 30,
		SuccessesToEnable: 2,
	})

	// Create endpoint with no models
	mockEp := endpoint.NewEndpointState("test-endpoint", "https://example.com/api", "test-key", "")
	mockEp.SetSupportedModels([]string{})
	hm.AddEndpoint(mockEp)

	handler := NewHandler(hm, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response ModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response.Data) != 0 {
		t.Errorf("expected 0 models, got %d", len(response.Data))
	}
}

func TestModelsHandlerRejectsNonGet(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})
	handler := NewHandler(hm, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}

	if w.Header().Get("Allow") != http.MethodGet {
		t.Errorf("expected Allow header with GET, got %s", w.Header().Get("Allow"))
	}
}

func TestModelsHandlerFiltersDisabledEndpoints(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{
		FailuresToDisable: 5,
		RecoveryProbeInterval: 30,
		SuccessesToEnable: 2,
	})

	// Create enabled endpoint with models
	enabledEp := endpoint.NewEndpointState("enabled-endpoint", "https://example.com/api", "test-key", "")
	enabledEp.SetSupportedModels([]string{"model-1"})
	hm.AddEndpoint(enabledEp)

	// Create disabled endpoint with models
	disabledEp := endpoint.NewEndpointState("disabled-endpoint", "https://example.com/api", "test-key", "")
	disabledEp.SetSupportedModels([]string{"model-2"})
	disabledEp.Disable()
	hm.AddEndpoint(disabledEp)

	handler := NewHandler(hm, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response ModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should only return model from enabled endpoint
	if len(response.Data) != 1 {
		t.Errorf("expected 1 model from enabled endpoint, got %d", len(response.Data))
	}

	if response.Data[0].ID != "model-1" {
		t.Errorf("expected model-1, got %s", response.Data[0].ID)
	}
}

func TestModelsHandlerReturnsUniqueModels(t *testing.T) {
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{
		FailuresToDisable: 5,
		RecoveryProbeInterval: 30,
		SuccessesToEnable: 2,
	})

	// Create two endpoints with overlapping models
	ep1 := endpoint.NewEndpointState("endpoint-1", "https://example.com/api", "test-key", "")
	ep1.SetSupportedModels([]string{"model-1", "model-2"})
	hm.AddEndpoint(ep1)

	ep2 := endpoint.NewEndpointState("endpoint-2", "https://example.com/api", "test-key", "")
	ep2.SetSupportedModels([]string{"model-2", "model-3"})
	hm.AddEndpoint(ep2)

	handler := NewHandler(hm, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response ModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return unique models: model-1, model-2, model-3
	if len(response.Data) != 3 {
		t.Errorf("expected 3 unique models, got %d", len(response.Data))
	}

	// Check models are unique
	modelSet := make(map[string]bool)
	for _, model := range response.Data {
		if modelSet[model.ID] {
			t.Errorf("duplicate model: %s", model.ID)
		}
		modelSet[model.ID] = true
	}
}
