package proxy

import (
	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/usage"
)

// usageFetcher provides cached usage data for endpoints.
type usageFetcher interface {
	GetAll() map[string]*usage.UsageData
}

// computeUsagePressure returns a value in [0.0, 1.0] representing how
// close an endpoint is to exhausting its quota. Higher = more pressure.
// Returns 0.0 for endpoints with no usage data.
func computeUsagePressure(data *usage.UsageData) float64 {
	if data == nil {
		return 0.0
	}

	maxPressure := 0.0
	for _, detail := range data.Details {
		if detail.IsAvailable != nil && !*detail.IsAvailable {
			return 1.0
		}
		if detail.Percentage > 0 {
			p := detail.Percentage / 100.0
			if p > 1.0 {
				p = 1.0
			}
			if p > maxPressure {
				maxPressure = p
			}
		}
	}

	return maxPressure
}

// selectEndpoint picks the endpoint with the lowest composite score:
//
//	score = active_connections + usage_pressure × virtualConnections
//
// Falls back to pure least-connections for endpoints without usage data.
func (h *Handler) selectEndpoint(model string, endpoints map[string]*endpoint.EndpointState, exclude map[string]bool) *endpoint.EndpointState {
	var allUsage map[string]*usage.UsageData
	if h.usageFetcher != nil {
		allUsage = h.usageFetcher.GetAll()
	}

	var selected *endpoint.EndpointState
	minScore := -1.0

	for _, ep := range endpoints {
		if exclude != nil && exclude[ep.Name] {
			continue
		}
		conns := float64(ep.GetConnectionCount(model))
		pressure := computeUsagePressure(allUsage[ep.Name])
		score := conns + pressure*h.usageVirtualConns

		if selected == nil || score < minScore {
			selected = ep
			minScore = score
		}
	}

	return selected
}

// filterFallbackEndpoints removes fallback endpoints from the map when at least
// one non-fallback endpoint is available. Returns whether any non-fallback endpoint exists.
func filterFallbackEndpoints(endpoints map[string]*endpoint.EndpointState) {
	hasNonFallback := false
	for _, ep := range endpoints {
		if !ep.Fallback {
			hasNonFallback = true
			break
		}
	}
	if !hasNonFallback {
		return
	}
	for name, ep := range endpoints {
		if ep.Fallback {
			delete(endpoints, name)
		}
	}
}
