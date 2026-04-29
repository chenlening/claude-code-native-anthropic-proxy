package routing

import (
	"testing"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
)

func newTestBackend(name, url string) *endpoint.EndpointState {
	return endpoint.NewEndpointState(name, "https://"+url+".com", "key")
}

func TestLeastConnectionsSelectsLowestCount(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 1},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 1},
		{Endpoint: newTestBackend("endpoint-c", "c"), BackendModel: "model-c", Weight: 1},
	}

	backends[0].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4") // 5

	backends[1].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[1].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[1].Endpoint.IncrementConnection("claude-sonnet-4") // 3

	backends[2].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[2].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[2].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[2].Endpoint.IncrementConnection("claude-sonnet-4") // 4

	selected := lb.Select("claude-sonnet-4", backends)
	if selected.Endpoint.Name != "endpoint-b" {
		t.Errorf("Select() = %q, want endpoint-b (lowest connections: 3)", selected.Endpoint.Name)
	}
}

func TestLeastConnectionsExcludesDisabled(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 1},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 1},
	}

	backends[0].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4") // 2
	backends[1].Endpoint.IncrementConnection("claude-sonnet-4") // 1, BUT DISABLED
	backends[1].Endpoint.Disable()

	selected := lb.Select("claude-sonnet-4", backends)
	if selected.Endpoint.Name != "endpoint-a" {
		t.Errorf("Select() = %q, want endpoint-a (endpoint-b is disabled)", selected.Endpoint.Name)
	}
}

func TestLeastConnectionsReturnsNilWhenAllDisabled(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 1},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 1},
	}
	backends[0].Endpoint.Disable()
	backends[1].Endpoint.Disable()

	selected := lb.Select("claude-sonnet-4", backends)
	if selected != nil {
		t.Errorf("Select() = %q, want nil (all backends disabled)", selected.Endpoint.Name)
	}
}

func TestLeastConnectionsEmptyListReturnsNil(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()
	selected := lb.Select("claude-sonnet-4", []*ModelBackend{})
	if selected != nil {
		t.Errorf("Select() = %v, want nil (empty list)", selected)
	}
}

func TestLeastConnectionsEqualWeightDefaultsToOne(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 0},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 0},
	}

	selected := lb.Select("claude-sonnet-4", backends)
	if selected == nil {
		t.Error("Select() should return an endpoint when both have same count")
	}
}

func TestLeastConnectionsSelectsWeightedBackend(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	// epA weight 10, epB weight 5
	// epB has 1 connection (effective load 0.2), epA has 0 (effective load 0.0)
	// epA should win
	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 10},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 5},
	}
	backends[1].Endpoint.IncrementConnection("claude-sonnet-4")

	selected := lb.Select("claude-sonnet-4", backends)
	if selected.Endpoint.Name != "endpoint-a" {
		t.Errorf("Select() = %q, want endpoint-a (effective load 0.0 < 0.2)", selected.Endpoint.Name)
	}
}

func TestLeastConnectionsZeroWeightDefaultsToOne(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	// Zero weight treated as 1
	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 0},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 2},
	}
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4") // effective load = 1/1 = 1.0
	backends[1].Endpoint.IncrementConnection("claude-sonnet-4") // effective load = 1/2 = 0.5

	selected := lb.Select("claude-sonnet-4", backends)
	if selected.Endpoint.Name != "endpoint-b" {
		t.Errorf("Select() = %q, want endpoint-b (effective load 0.5 < 1.0)", selected.Endpoint.Name)
	}
}

func TestLeastConnectionsSelectWithExclusionWeighted(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 10},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 5},
	}
	backends[0].Endpoint.IncrementConnection("claude-sonnet-4")
	backends[1].Endpoint.IncrementConnection("claude-sonnet-4")

	exclude := map[string]bool{"endpoint-b": true}
	selected := lb.SelectWithExclusion("claude-sonnet-4", backends, exclude)
	if selected.Endpoint.Name != "endpoint-a" {
		t.Errorf("SelectWithExclusion() = %q, want endpoint-a", selected.Endpoint.Name)
	}
}

func TestLeastConnectionsSelectWithExclusionWeightedAllDisabled(t *testing.T) {
	lb := NewLeastConnectionsLoadBalancer()

	backends := []*ModelBackend{
		{Endpoint: newTestBackend("endpoint-a", "a"), BackendModel: "model-a", Weight: 10},
		{Endpoint: newTestBackend("endpoint-b", "b"), BackendModel: "model-b", Weight: 5},
	}
	backends[0].Endpoint.Disable()
	backends[1].Endpoint.Disable()

	selected := lb.SelectWithExclusion("claude-sonnet-4", backends, map[string]bool{})
	if selected != nil {
		t.Errorf("SelectWithExclusion() = %q, want nil (all disabled)", selected.Endpoint.Name)
	}
}
