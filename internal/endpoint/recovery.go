package endpoint

import (
	"time"
)

// RunRecoveryProbe periodically checks disabled endpoints
func (h *HealthManager) RunRecoveryProbe(stopCh <-chan struct{}) {
	ticker := time.NewTicker(h.config.RecoveryProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			h.probeDisabledEndpoints()
		}
	}
}

// probeDisabledEndpoints checks health of all disabled endpoints
func (h *HealthManager) probeDisabledEndpoints() {
	disabled := h.GetDisabledEndpoints()

	for _, ep := range disabled {
		err := ep.Verify(ep.ProbeModel)
		healthy := err == nil
		reason := ""
		if err != nil {
			reason = err.Error()
		}

		ep.mu.Lock()
		ep.lastProbeTime = time.Now()
		ep.lastProbeSuccess = healthy
		if !healthy {
			ep.successesSince = 0
			ep.lastFailureReason = reason
			ep.lastFailureTime = time.Now()
		}
		ep.mu.Unlock()

		if healthy {
			h.RecordSuccess(ep)
		}
	}
}
