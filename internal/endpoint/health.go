package endpoint

import (
	"sync"
	"time"
)

// HealthConfig holds health management configuration
type HealthConfig struct {
	FailuresToDisable     int
	RecoveryProbeInterval time.Duration
	SuccessesToEnable     int
}

// HealthManager manages endpoint health state
type HealthManager struct {
	config HealthConfig

	mu        sync.RWMutex
	endpoints map[string]*EndpointState
}

// NewHealthManager creates a new health manager
func NewHealthManager(config HealthConfig) *HealthManager {
	return &HealthManager{
		config:    config,
		endpoints: make(map[string]*EndpointState),
	}
}

// AddEndpoint registers an endpoint with the health manager
func (h *HealthManager) AddEndpoint(ep *EndpointState) {
	h.mu.Lock()
	h.endpoints[ep.Name] = ep
	h.mu.Unlock()
}

// RecordFailure records a failure for an endpoint and may disable it
func (h *HealthManager) RecordFailure(ep *EndpointState, reason ...string) {
	ep.RecordFailure(reason...)

	if ep.ShouldDisable(h.config.FailuresToDisable) {
		ep.Disable()
	}
}

// RecordSuccess records a success for an endpoint and may re-enable it
func (h *HealthManager) RecordSuccess(ep *EndpointState) {
	ep.RecordSuccess()

	if ep.IsDisabled() && ep.ShouldEnable(h.config.SuccessesToEnable) {
		ep.Enable()
		ep.ResetFailures()
	}
}

// GetHealthyEndpoints returns all non-disabled endpoints
func (h *HealthManager) GetHealthyEndpoints() []*EndpointState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var healthy []*EndpointState
	for _, ep := range h.endpoints {
		if !ep.IsDisabled() {
			healthy = append(healthy, ep)
		}
	}
	return healthy
}

// IsEndpointHealthy returns true if endpoint is not disabled
func (h *HealthManager) IsEndpointHealthy(ep *EndpointState) bool {
	return !ep.IsDisabled()
}

// GetDisabledEndpoints returns all disabled endpoints
func (h *HealthManager) GetDisabledEndpoints() []*EndpointState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var disabled []*EndpointState
	for _, ep := range h.endpoints {
		if ep.IsDisabled() {
			disabled = append(disabled, ep)
		}
	}
	return disabled
}

// GetEndpoint returns an endpoint by name
func (h *HealthManager) GetEndpoint(name string) *EndpointState {
	h.mu.RLock()
	ep := h.endpoints[name]
	h.mu.RUnlock()
	return ep
}

// GetAllEndpoints returns all registered endpoints
func (h *HealthManager) GetAllEndpoints() []*EndpointState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	endpoints := make([]*EndpointState, 0, len(h.endpoints))
	for _, ep := range h.endpoints {
		endpoints = append(endpoints, ep)
	}
	return endpoints
}
