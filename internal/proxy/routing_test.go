package proxy

import (
	"testing"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/usage"
)

func TestComputeUsagePressure(t *testing.T) {
	t.Run("nil data returns zero", func(t *testing.T) {
		if p := computeUsagePressure(nil); p != 0.0 {
			t.Errorf("got %f, want 0.0", p)
		}
	})

	t.Run("no details returns zero", func(t *testing.T) {
		data := &usage.UsageData{Provider: "test"}
		if p := computeUsagePressure(data); p != 0.0 {
			t.Errorf("got %f, want 0.0", p)
		}
	})

	t.Run("percentage-based pressure", func(t *testing.T) {
		data := &usage.UsageData{
			Details: []usage.LimitDetail{
				{Type: "TOKENS_LIMIT", Percentage: 80.0},
			},
		}
		if p := computeUsagePressure(data); p != 0.8 {
			t.Errorf("got %f, want 0.8", p)
		}
	})

	t.Run("max pressure across multiple details", func(t *testing.T) {
		data := &usage.UsageData{
			Details: []usage.LimitDetail{
				{Type: "TOKENS_LIMIT", Percentage: 30.0},
				{Type: "TIME_LIMIT", Percentage: 70.0},
			},
		}
		if p := computeUsagePressure(data); p != 0.7 {
			t.Errorf("got %f, want 0.7", p)
		}
	})

	t.Run("is_available false returns 1.0", func(t *testing.T) {
		falsy := false
		data := &usage.UsageData{
			Details: []usage.LimitDetail{
				{Type: "BALANCE", Percentage: 10.0, IsAvailable: &falsy},
			},
		}
		if p := computeUsagePressure(data); p != 1.0 {
			t.Errorf("got %f, want 1.0", p)
		}
	})

	t.Run("percentage over 100 clamped to 1.0", func(t *testing.T) {
		data := &usage.UsageData{
			Details: []usage.LimitDetail{
				{Type: "TOKENS_LIMIT", Percentage: 150.0},
			},
		}
		if p := computeUsagePressure(data); p != 1.0 {
			t.Errorf("got %f, want 1.0", p)
		}
	})

	t.Run("zero percentage ignored", func(t *testing.T) {
		data := &usage.UsageData{
			Details: []usage.LimitDetail{
				{Type: "BALANCE", Percentage: 0.0},
			},
		}
		if p := computeUsagePressure(data); p != 0.0 {
			t.Errorf("got %f, want 0.0", p)
		}
	})
}

func TestSelectEndpoint(t *testing.T) {
	t.Run("prefers endpoint with fewer connections", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://localhost", "key", "")
		ep1.IncrementConnection("claude-sonnet-4")

		ep2 := endpoint.NewEndpointState("ep2", "http://localhost", "key", "")

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
			"ep2": ep2,
		}

		h := &Handler{usageVirtualConns: 10}
		selected := h.selectEndpoint("claude-sonnet-4", endpoints, nil)
		if selected != ep2 {
			t.Error("should prefer endpoint with fewer connections")
		}
	})

	t.Run("prefers endpoint with lower usage at same connections", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://localhost", "key", "")
		ep2 := endpoint.NewEndpointState("ep2", "http://localhost", "key", "")

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
			"ep2": ep2,
		}

		fetcher := &mockFetcher{data: map[string]*usage.UsageData{
			"ep1": {Details: []usage.LimitDetail{{Percentage: 80.0}}},
			"ep2": {Details: []usage.LimitDetail{{Percentage: 20.0}}},
		}}

		h := &Handler{usageFetcher: fetcher, usageVirtualConns: 10}
		selected := h.selectEndpoint("claude-sonnet-4", endpoints, nil)
		if selected != ep2 {
			t.Error("should prefer endpoint with lower usage (20% vs 80%)")
		}
	})

	t.Run("excludes endpoints in exclude map", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://localhost", "key", "")
		ep2 := endpoint.NewEndpointState("ep2", "http://localhost", "key", "")

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
			"ep2": ep2,
		}

		h := &Handler{usageVirtualConns: 10}
		selected := h.selectEndpoint("claude-sonnet-4", endpoints, map[string]bool{"ep2": true})
		if selected != ep1 {
			t.Error("should skip excluded endpoint")
		}
	})

	t.Run("returns nil when all excluded", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://localhost", "key", "")

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
		}

		h := &Handler{usageVirtualConns: 10}
		selected := h.selectEndpoint("claude-sonnet-4", endpoints, map[string]bool{"ep1": true})
		if selected != nil {
			t.Error("should return nil when all endpoints excluded")
		}
	})

	t.Run("zero virtual connections disables usage awareness", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://localhost", "key", "")
		ep2 := endpoint.NewEndpointState("ep2", "http://localhost", "key", "")

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
			"ep2": ep2,
		}

		fetcher := &mockFetcher{data: map[string]*usage.UsageData{
			"ep1": {Details: []usage.LimitDetail{{Percentage: 90.0}}},
			"ep2": {Details: []usage.LimitDetail{{Percentage: 10.0}}},
		}}

		h := &Handler{usageFetcher: fetcher, usageVirtualConns: 0}
		selected := h.selectEndpoint("claude-sonnet-4", endpoints, nil)
		if selected == nil {
			t.Error("should return an endpoint")
		}
	})
}

type mockFetcher struct {
	data map[string]*usage.UsageData
}

func (m *mockFetcher) GetAll() map[string]*usage.UsageData {
	return m.data
}

func TestFilterFallbackEndpoints(t *testing.T) {
	t.Run("removes fallback when non-fallback present", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://1", "k", "")
		ep2 := endpoint.NewEndpointState("ep2", "http://2", "k", "")
		ep2.Fallback = true

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
			"ep2": ep2,
		}
		filterFallbackEndpoints(endpoints)

		if _, ok := endpoints["ep2"]; ok {
			t.Error("fallback endpoint should be removed when non-fallback exists")
		}
		if _, ok := endpoints["ep1"]; !ok {
			t.Error("non-fallback endpoint should remain")
		}
	})

	t.Run("keeps fallback when it's the only option", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://1", "k", "")
		ep1.Fallback = true

		endpoints := map[string]*endpoint.EndpointState{"ep1": ep1}
		filterFallbackEndpoints(endpoints)

		if _, ok := endpoints["ep1"]; !ok {
			t.Error("fallback endpoint should be kept when no alternative exists")
		}
	})

	t.Run("keeps all fallbacks when only fallbacks exist", func(t *testing.T) {
		ep1 := endpoint.NewEndpointState("ep1", "http://1", "k", "")
		ep2 := endpoint.NewEndpointState("ep2", "http://2", "k", "")
		ep1.Fallback = true
		ep2.Fallback = true

		endpoints := map[string]*endpoint.EndpointState{
			"ep1": ep1,
			"ep2": ep2,
		}
		filterFallbackEndpoints(endpoints)

		if len(endpoints) != 2 {
			t.Errorf("all fallback endpoints should be kept when no non-fallback, got %d", len(endpoints))
		}
	})

	t.Run("empty map is safe", func(t *testing.T) {
		endpoints := map[string]*endpoint.EndpointState{}
		filterFallbackEndpoints(endpoints)
		if len(endpoints) != 0 {
			t.Error("empty map should remain empty")
		}
	})
}
