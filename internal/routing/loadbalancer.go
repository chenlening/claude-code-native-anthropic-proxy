package routing

// LoadBalancer selects an endpoint from a pool
type LoadBalancer interface {
	Select(model string, backends []*ModelBackend) *ModelBackend
	SelectWithExclusion(model string, backends []*ModelBackend, exclude map[string]bool) *ModelBackend
}

// LeastConnectionsLoadBalancer selects the endpoint with lowest effective load (connections/weight)
type LeastConnectionsLoadBalancer struct{}

// NewLeastConnectionsLoadBalancer creates a new least-connections load balancer
func NewLeastConnectionsLoadBalancer() *LeastConnectionsLoadBalancer {
	return &LeastConnectionsLoadBalancer{}
}

// Select returns the backend with lowest effective load (connections/weight) for the model
func (lb *LeastConnectionsLoadBalancer) Select(model string, backends []*ModelBackend) *ModelBackend {
	if len(backends) == 0 {
		return nil
	}

	var selected *ModelBackend
	minLoad := float64(-1)

	for _, b := range backends {
		if b.Endpoint.IsDisabled() {
			continue
		}
		weight := b.Weight
		if weight <= 0 {
			weight = 1 // zero or negative weight defaults to 1
		}
		count := b.Endpoint.GetConnectionCount(model)
		effectiveLoad := float64(count) / float64(weight)

		if selected == nil || effectiveLoad < minLoad {
			selected = b
			minLoad = effectiveLoad
		}
	}

	return selected
}

// SelectWithExclusion returns the backend with lowest effective load, skipping excluded endpoints
func (lb *LeastConnectionsLoadBalancer) SelectWithExclusion(model string, backends []*ModelBackend, exclude map[string]bool) *ModelBackend {
	if len(backends) == 0 {
		return nil
	}

	var selected *ModelBackend
	minLoad := float64(-1)

	for _, b := range backends {
		if b.Endpoint.IsDisabled() {
			continue
		}
		if exclude[b.Endpoint.Name] {
			continue
		}
		weight := b.Weight
		if weight <= 0 {
			weight = 1
		}
		count := b.Endpoint.GetConnectionCount(model)
		effectiveLoad := float64(count) / float64(weight)

		if selected == nil || effectiveLoad < minLoad {
			selected = b
			minLoad = effectiveLoad
		}
	}

	return selected
}
