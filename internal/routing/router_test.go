package routing

import (
	"testing"

	"github.com/anthropic-transparent-proxy/internal/config"
	"github.com/anthropic-transparent-proxy/internal/endpoint"
)

func TestModelRouterResolveModel(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4-20250514": {
				Backends: []config.BackendConfig{
					{Endpoint: "endpoint-a", Model: "claude-sonnet-4-20250514", Weight: 10},
					{Endpoint: "endpoint-b", Model: "custom-sonnet", Weight: 5},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"endpoint-a": {URL: "https://api.anthropic.com", APIKey: "key-a"},
			"endpoint-b": {URL: "https://custom.com", APIKey: "key-b"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{FailuresToDisable: 5})
	epA := endpoint.NewEndpointState("endpoint-a", "https://api.anthropic.com", "key-a")
	epB := endpoint.NewEndpointState("endpoint-b", "https://custom.com", "key-b")
	hm.AddEndpoint(epA)
	hm.AddEndpoint(epB)

	router := NewModelRouter(cfg, hm, NewLeastConnectionsLoadBalancer())

	pool, err := router.Resolve("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if pool.FrontendModel != "claude-sonnet-4-20250514" {
		t.Errorf("FrontendModel = %q, want claude-sonnet-4-20250514", pool.FrontendModel)
	}
	if len(pool.Backends) != 2 {
		t.Errorf("Backends count = %d, want 2", len(pool.Backends))
	}
}

func TestModelRouterUnknownModel(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "endpoint-a", Model: "claude-sonnet-4"},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"endpoint-a": {URL: "https://api.anthropic.com", APIKey: "key"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})
	router := NewModelRouter(cfg, hm, NewLeastConnectionsLoadBalancer())

	_, err := router.Resolve("unknown-model")
	if err == nil {
		t.Error("Resolve() expected error for unknown model")
	}
}

func TestModelRouterSelectBackend(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "endpoint-a", Model: "claude-sonnet-4", Weight: 10},
					{Endpoint: "endpoint-b", Model: "custom-sonnet", Weight: 5},
					{Endpoint: "endpoint-c", Model: "sonnet-v4", Weight: 5},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"endpoint-a": {URL: "https://api.anthropic.com", APIKey: "key"},
			"endpoint-b": {URL: "https://custom.com", APIKey: "key"},
			"endpoint-c": {URL: "https://another.com", APIKey: "key"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})

	epA := endpoint.NewEndpointState("endpoint-a", "https://api.anthropic.com", "key")
	epB := endpoint.NewEndpointState("endpoint-b", "https://custom.com", "key")
	epC := endpoint.NewEndpointState("endpoint-c", "https://another.com", "key")

	epA.IncrementConnection("claude-sonnet-4")
	epA.IncrementConnection("claude-sonnet-4")
	epA.IncrementConnection("claude-sonnet-4") // 3

	epB.IncrementConnection("claude-sonnet-4") // 1

	epC.IncrementConnection("claude-sonnet-4")
	epC.IncrementConnection("claude-sonnet-4") // 2

	hm.AddEndpoint(epA)
	hm.AddEndpoint(epB)
	hm.AddEndpoint(epC)

	router := NewModelRouter(cfg, hm, NewLeastConnectionsLoadBalancer())

	backend, err := router.SelectBackend("claude-sonnet-4")
	if err != nil {
		t.Fatalf("SelectBackend() error = %v", err)
	}

	if backend.Endpoint.Name != "endpoint-b" {
		t.Errorf("Selected endpoint = %q, want endpoint-b (lowest connections)", backend.Endpoint.Name)
	}
	if backend.BackendModel != "custom-sonnet" {
		t.Errorf("BackendModel = %q, want custom-sonnet", backend.BackendModel)
	}
}

func TestModelRouterSelectBackendAllDisabled(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "endpoint-a", Model: "claude-sonnet-4"},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"endpoint-a": {URL: "https://api.anthropic.com", APIKey: "key"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})
	epA := endpoint.NewEndpointState("endpoint-a", "https://api.anthropic.com", "key")
	epA.Disable()
	hm.AddEndpoint(epA)

	router := NewModelRouter(cfg, hm, NewLeastConnectionsLoadBalancer())

	_, err := router.SelectBackend("claude-sonnet-4")
	if err == nil {
		t.Error("SelectBackend() expected error when all endpoints disabled")
	}
}

func TestModelRouterSelectBackendWeighted(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"claude-sonnet-4": {
				Backends: []config.BackendConfig{
					{Endpoint: "endpoint-a", Model: "model-a", Weight: 10},
					{Endpoint: "endpoint-b", Model: "model-b", Weight: 5},
				},
			},
		},
		Endpoints: map[string]config.EndpointConfig{
			"endpoint-a": {URL: "https://a.com", APIKey: "key"},
			"endpoint-b": {URL: "https://b.com", APIKey: "key"},
		},
	}

	hm := endpoint.NewHealthManager(endpoint.HealthConfig{})

	epA := endpoint.NewEndpointState("endpoint-a", "https://a.com", "key")
	epB := endpoint.NewEndpointState("endpoint-b", "https://b.com", "key")

	// epB has 1 connection, epA has 0
	// effective load: epA = 0/10 = 0.0, epB = 1/5 = 0.2
	// epA should be selected
	epB.IncrementConnection("claude-sonnet-4")

	hm.AddEndpoint(epA)
	hm.AddEndpoint(epB)

	router := NewModelRouter(cfg, hm, NewLeastConnectionsLoadBalancer())

	backend, err := router.SelectBackend("claude-sonnet-4")
	if err != nil {
		t.Fatalf("SelectBackend() error = %v", err)
	}
	if backend.Endpoint.Name != "endpoint-a" {
		t.Errorf("Selected backend = %q, want endpoint-a (effective load 0.0 < 0.2)", backend.Endpoint.Name)
	}
}
