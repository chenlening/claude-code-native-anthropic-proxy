package routing

import (
	"fmt"

	"github.com/anthropic-transparent-proxy/internal/config"
	"github.com/anthropic-transparent-proxy/internal/endpoint"
)

// ModelBackend represents a backend endpoint for a model
type ModelBackend struct {
	Endpoint     *endpoint.EndpointState
	BackendModel string
	Weight       int
}

// ModelPool represents a pool of backends for a frontend model
type ModelPool struct {
	FrontendModel string
	Strategy      string
	Backends      []*ModelBackend
}

// ModelRouter resolves frontend models to backend pools
type ModelRouter struct {
	config       *config.Config
	healthMgr    *endpoint.HealthManager
	loadBalancer LoadBalancer
	pools        map[string]*ModelPool
}

// NewModelRouter creates a new model router
func NewModelRouter(cfg *config.Config, hm *endpoint.HealthManager, lb LoadBalancer) *ModelRouter {
	router := &ModelRouter{
		config:       cfg,
		healthMgr:    hm,
		loadBalancer: lb,
		pools:        make(map[string]*ModelPool),
	}

	router.buildPools()
	return router
}

// buildPools constructs model pools from configuration
func (r *ModelRouter) buildPools() {
	for frontendModel, modelCfg := range r.config.Models {
		pool := &ModelPool{
			FrontendModel: frontendModel,
			Strategy:      modelCfg.Strategy,
			Backends:      make([]*ModelBackend, 0, len(modelCfg.Backends)),
		}

		for _, backendCfg := range modelCfg.Backends {
			ep := r.healthMgr.GetEndpoint(backendCfg.Endpoint)
			if ep == nil {
				continue
			}

			pool.Backends = append(pool.Backends, &ModelBackend{
				Endpoint:     ep,
				BackendModel: backendCfg.Model,
				Weight:       backendCfg.Weight,
			})
		}

		r.pools[frontendModel] = pool
	}
}

// Resolve returns the model pool for a frontend model
func (r *ModelRouter) Resolve(frontendModel string) (*ModelPool, error) {
	pool, exists := r.pools[frontendModel]
	if !exists {
		return nil, fmt.Errorf("unknown model: %s", frontendModel)
	}
	return pool, nil
}

// SelectBackend selects the best backend for a frontend model
func (r *ModelRouter) SelectBackend(frontendModel string) (*ModelBackend, error) {
	pool, err := r.Resolve(frontendModel)
	if err != nil {
		return nil, err
	}

	selected := r.loadBalancer.Select(frontendModel, pool.Backends)
	if selected == nil {
		return nil, fmt.Errorf("no healthy backends for model: %s", frontendModel)
	}
	return selected, nil
}

// SelectBackendWithExclusion selects the best backend for a frontend model,
// excluding endpoints in the exclude set
func (r *ModelRouter) SelectBackendWithExclusion(frontendModel string, exclude map[string]bool) (*ModelBackend, error) {
	pool, err := r.Resolve(frontendModel)
	if err != nil {
		return nil, err
	}

	selected := r.loadBalancer.SelectWithExclusion(frontendModel, pool.Backends, exclude)
	if selected == nil {
		return nil, fmt.Errorf("no healthy backends for model: %s", frontendModel)
	}
	return selected, nil
}

// GetAllPools returns all model pools
func (r *ModelRouter) GetAllPools() map[string]*ModelPool {
	return r.pools
}
