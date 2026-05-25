package endpoint

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// EndpointState holds runtime state for an endpoint
type EndpointState struct {
	Name            string
	URL             string
	ModelsEndpoint  string // Optional: custom URL for model discovery
	APIKey          string
	Client          *http.Client

	// Mutable state protected by mutex
	mu             sync.RWMutex
	connections    map[string]int64 // model → active connection count
	failures       int64
	successes      int64
	disabled       bool
	successesSince int64 // Successes since last failure (for recovery)

	// Error tracking
	lastFailureTime   time.Time
	lastFailureReason string
	lastRequestTime   time.Time

	// Model discovery error tracking (separate from request failures)
	lastDiscoveryError string
	lastDiscoveryTime  time.Time

	// Probe tracking
	lastProbeTime    time.Time
	lastProbeSuccess bool
	ProbeModel       string // Backend model name to use in health probes

	// SupportedModels lists the models this endpoint supports
	SupportedModels []string
}

// NewEndpointState creates a new endpoint state
func NewEndpointState(name, url, apiKey, modelsEndpoint string) *EndpointState {
	return &EndpointState{
		Name:           name,
		URL:            url,
		ModelsEndpoint: modelsEndpoint,
		APIKey:         apiKey,
		Client: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 90 * time.Second,
			},
		},
		connections: make(map[string]int64),
	}
}

// WithTimeout sets the response header timeout on the transport
func (e *EndpointState) WithTimeout(timeout time.Duration) *EndpointState {
	e.mu.Lock()
	if transport, ok := e.Client.Transport.(*http.Transport); ok {
		transport.ResponseHeaderTimeout = timeout
	}
	e.mu.Unlock()
	return e
}

// IncrementConnection increases the connection count for a model
func (e *EndpointState) IncrementConnection(model string) {
	e.mu.Lock()
	e.connections[model]++
	e.mu.Unlock()
}

// DecrementConnection decreases the connection count for a model
func (e *EndpointState) DecrementConnection(model string) {
	e.mu.Lock()
	if e.connections[model] > 0 {
		e.connections[model]--
	}
	e.mu.Unlock()
}

// GetConnectionCount returns the current connection count for a model
func (e *EndpointState) GetConnectionCount(model string) int64 {
	e.mu.RLock()
	count := e.connections[model]
	e.mu.RUnlock()
	return count
}

// GetTotalConnections returns total connections across all models
func (e *EndpointState) GetTotalConnections() int64 {
	e.mu.RLock()
	var total int64
	for _, count := range e.connections {
		total += count
	}
	e.mu.RUnlock()
	return total
}

// Disable marks the endpoint as disabled
func (e *EndpointState) Disable() {
	e.mu.Lock()
	e.disabled = true
	e.mu.Unlock()
}

// Enable marks the endpoint as enabled
func (e *EndpointState) Enable() {
	e.mu.Lock()
	e.disabled = false
	e.successesSince = 0
	e.mu.Unlock()
}

// IsDisabled returns whether the endpoint is disabled
func (e *EndpointState) IsDisabled() bool {
	e.mu.RLock()
	disabled := e.disabled
	e.mu.RUnlock()
	return disabled
}

// Verify checks if the endpoint is reachable by making a real test request
// Returns nil if successful, error if not reachable
// modelName should be a valid model for this endpoint from the config
func (e *EndpointState) Verify(modelName string) error {
	body := []byte(`{"model":"` + modelName + `","max_tokens":1,"messages":[{"role":"user","content":"test"}]}`)
	req, err := http.NewRequest("POST", e.URL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := e.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Only 200 OK is considered healthy; 401 (auth error), 429 (rate limit), and 5xx are unhealthy
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("status=%d body=%q", resp.StatusCode, string(respBody))
	}
	return nil
}

// RecordFailure records a failure, optionally with a reason string (e.g. status code or "network_error")
func (e *EndpointState) RecordFailure(reason ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failures++
	e.successesSince = 0
	e.lastFailureTime = time.Now()
	if len(reason) > 0 {
		e.lastFailureReason = reason[0]
	}
}

// ResetFailures clears the failure count and associated error info
func (e *EndpointState) ResetFailures() {
	e.mu.Lock()
	e.failures = 0
	e.lastFailureTime = time.Time{}
	e.lastFailureReason = ""
	e.mu.Unlock()
}

// GetFailures returns the current failure count
func (e *EndpointState) GetFailures() int64 {
	e.mu.RLock()
	count := e.failures
	e.mu.RUnlock()
	return count
}

// RecordSuccess increments the success count and updates last request time
func (e *EndpointState) RecordSuccess() {
	e.mu.Lock()
	e.successes++
	e.successesSince++
	e.lastRequestTime = time.Now()
	e.mu.Unlock()
}

// GetSuccessesSince returns successes since last failure
func (e *EndpointState) GetSuccessesSince() int64 {
	e.mu.RLock()
	count := e.successesSince
	e.mu.RUnlock()
	return count
}

// GetLastFailureTime returns the timestamp of the last failure
func (e *EndpointState) GetLastFailureTime() time.Time {
	e.mu.RLock()
	t := e.lastFailureTime
	e.mu.RUnlock()
	return t
}

// GetLastFailureReason returns the reason of the last failure
func (e *EndpointState) GetLastFailureReason() string {
	e.mu.RLock()
	r := e.lastFailureReason
	e.mu.RUnlock()
	return r
}

// GetLastRequestTime returns the timestamp of the last request
func (e *EndpointState) GetLastRequestTime() time.Time {
	e.mu.RLock()
	t := e.lastRequestTime
	e.mu.RUnlock()
	return t
}

// GetLastProbeTime returns when the last recovery probe ran
func (e *EndpointState) GetLastProbeTime() time.Time {
	e.mu.RLock()
	t := e.lastProbeTime
	e.mu.RUnlock()
	return t
}

// GetLastProbeSuccess returns whether the last probe succeeded
func (e *EndpointState) GetLastProbeSuccess() bool {
	e.mu.RLock()
	s := e.lastProbeSuccess
	e.mu.RUnlock()
	return s
}

// ShouldDisable returns true if failures exceed threshold
func (e *EndpointState) ShouldDisable(threshold int) bool {
	e.mu.RLock()
	failures := e.failures
	e.mu.RUnlock()
	return failures >= int64(threshold)
}

// ShouldEnable returns true if successes exceed threshold
func (e *EndpointState) ShouldEnable(threshold int) bool {
	e.mu.RLock()
	successes := e.successesSince
	e.mu.RUnlock()
	return successes >= int64(threshold)
}

// SetDiscoveryError records a model discovery error
func (e *EndpointState) SetDiscoveryError(errMsg string) {
	e.mu.Lock()
	e.lastDiscoveryError = errMsg
	e.lastDiscoveryTime = time.Now()
	e.mu.Unlock()
}

// GetDiscoveryError returns the last model discovery error
func (e *EndpointState) GetDiscoveryError() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastDiscoveryError
}

// GetDiscoveryTime returns when the last model discovery error occurred
func (e *EndpointState) GetDiscoveryTime() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastDiscoveryTime
}

// SetSupportedModels sets the list of supported models
func (e *EndpointState) SetSupportedModels(models []string) {
	e.mu.Lock()
	e.SupportedModels = models
	e.mu.Unlock()
}

// GetSupportedModels returns a copy of the supported models list
func (e *EndpointState) GetSupportedModels() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]string, len(e.SupportedModels))
	copy(result, e.SupportedModels)
	return result
}

// SupportsModel returns true if the endpoint supports the given model
func (e *EndpointState) SupportsModel(model string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, m := range e.SupportedModels {
		if m == model {
			return true
		}
	}
	return false
}
