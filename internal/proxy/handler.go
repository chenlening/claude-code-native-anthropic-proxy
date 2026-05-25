package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/metrics"
)

// Handler is the main proxy HTTP handler
type Handler struct {
	healthMgr *endpoint.HealthManager
	metrics   *metrics.Metrics
	logger    *slog.Logger
}

// NewHandler creates a new proxy handler
func NewHandler(
	healthMgr *endpoint.HealthManager,
	metrics *metrics.Metrics,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		healthMgr: healthMgr,
		metrics:   metrics,
		logger:    logger,
	}
}

// ServeHTTP handles incoming Anthropic API requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Read request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read request body", "error", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Extract model from request
	frontendModel, parsedReq, err := ParseRequest(bodyBytes)
	if err != nil {
		h.logger.Error("failed to parse request", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get endpoints that support this model
	supportedEndpointNames := h.healthMgr.GetEndpointsForModel(frontendModel)
	if len(supportedEndpointNames) == 0 {
		h.logger.Error("model not supported by any endpoint", "model", frontendModel)
		http.Error(w, "model not supported by any endpoint", http.StatusServiceUnavailable)
		return
	}

	// Build endpoint map for quick lookup
	supportedEndpoints := make(map[string]*endpoint.EndpointState)
	for _, name := range supportedEndpointNames {
		ep := h.healthMgr.GetEndpoint(name)
		if ep != nil && !ep.IsDisabled() {
			supportedEndpoints[name] = ep
		}
	}

	if len(supportedEndpoints) == 0 {
		h.logger.Error("all endpoints supporting model are disabled", "model", frontendModel)
		http.Error(w, "model not supported by any endpoint", http.StatusServiceUnavailable)
		return
	}

	maxAttempts := len(supportedEndpoints)

	// Retry loop for 429 responses
	attempted := make(map[string]bool)

	var resp *http.Response
	var selectedEp *endpoint.EndpointState

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Select least-connections endpoint from supported endpoints
		ep := h.selectLeastConnectionsEndpoint(frontendModel, supportedEndpoints, attempted)
		if ep == nil {
			break // no more endpoints available
		}
		attempted[ep.Name] = true

		h.logger.Info("routing request",
			"frontend_model", frontendModel,
			"endpoint", ep.Name,
			"attempt", attempt+1,
		)

		// Track connection
		ep.IncrementConnection(frontendModel)

		// Create upstream request - forward body unchanged (no model rewriting)
		upstreamReq, err := http.NewRequest(r.Method, ep.URL+r.URL.Path, nil)
		if err != nil {
			ep.DecrementConnection(frontendModel)
			h.logger.Error("failed to create upstream request", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Propagate client context so upstream is cancelled if client disconnects
		upstreamReq = upstreamReq.WithContext(r.Context())

		// Set body from original content (no model replacement)
		upstreamReq.ContentLength = int64(len(bodyBytes))
		upstreamReq.Body = &closingReader{src: bodyBytes}

		// Copy headers, replacing auth
		for key, values := range r.Header {
			for _, value := range values {
				lowerKey := key
				if lowerKey == "X-Api-Key" || lowerKey == "Authorization" {
					continue
				}
				upstreamReq.Header.Add(key, value)
			}
		}
		upstreamReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
		upstreamReq.Header.Set("Anthropic-Version", r.Header.Get("Anthropic-Version"))

		// Forward request
		resp, err = ep.Client.Do(upstreamReq)
		if err != nil {
			ep.DecrementConnection(frontendModel)
			h.logger.Error("upstream request failed", "endpoint", ep.Name, "error", err)
			h.healthMgr.RecordFailure(ep, "network_error")
			h.metrics.RecordRequest(frontendModel, frontendModel, ep.Name, time.Since(start).Seconds(), false)
			continue
		}

		// If not 429, this is our final response
		if resp.StatusCode != 429 {
			selectedEp = ep
			break
		}

		// 429 — read body for failure reason, close, decrement, record, try next endpoint
		reason := fmt.Sprintf("status=429 body=%q", readBodyForReason(resp.Body))
		h.healthMgr.RecordFailure(ep, reason)
		ep.DecrementConnection(frontendModel)
		h.logger.Warn("rate limited, retrying on next endpoint",
			"endpoint", ep.Name, "attempt", attempt+1)
	}

	if resp == nil {
		h.logger.Error("all endpoints exhausted", "model", frontendModel)
		http.Error(w, "no backend available", http.StatusServiceUnavailable)
		return
	}

	defer resp.Body.Close()
	defer selectedEp.DecrementConnection(frontendModel)

	// Record success/failure based on status code (all non-2xx are failures)
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	var errBody []byte
	if success {
		h.healthMgr.RecordSuccess(selectedEp)
	} else {
		errBody, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		reason := fmt.Sprintf("status=%d body=%q", resp.StatusCode, truncateString(string(errBody), 200))
		h.healthMgr.RecordFailure(selectedEp, reason)

		// DEBUG: Log request body for 4xx errors to help diagnose tool_result issues
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			h.logger.Warn("backend returned client error",
				"endpoint", selectedEp.Name,
				"status", resp.StatusCode,
				"error_body", truncateString(string(errBody), 500),
				"request_has_tools", len(parsedReq.Tools) > 0,
				"request_messages_len", len(parsedReq.Messages),
			)
		}
	}

	h.metrics.RecordRequest(frontendModel, frontendModel, selectedEp.Name, time.Since(start).Seconds(), success)

	// Copy response headers, but remove Content-Length since we may modify the body
	for key, values := range resp.Header {
		if key == "Content-Length" {
			continue // Skip Content-Length - we'll recalculate or use chunked encoding
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Stream response body directly (no model replacement)
	if parsedReq.Stream {
		flusher, canFlush := w.(http.Flusher)
		buf := make([]byte, 32*1024)

		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					break
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil {
				break
			}
		}
	} else {
		// Non-streaming response - read entire body and send
		var body []byte
		if errBody != nil {
			// Error response already read
			body = errBody
		} else {
			// Success response - read body
			body, err = io.ReadAll(resp.Body)
			if err != nil {
				h.logger.Error("failed to read response body", "error", err)
				http.Error(w, "failed to read response", http.StatusInternalServerError)
				return
			}
		}

		w.Write(body)
	}
}

// selectLeastConnectionsEndpoint selects the endpoint with the lowest connection count
func (h *Handler) selectLeastConnectionsEndpoint(model string, endpoints map[string]*endpoint.EndpointState, exclude map[string]bool) *endpoint.EndpointState {
	var selected *endpoint.EndpointState
	minLoad := float64(-1)

	for _, ep := range endpoints {
		if exclude[ep.Name] {
			continue
		}
		count := ep.GetConnectionCount(model)
		if selected == nil || float64(count) < minLoad {
			selected = ep
			minLoad = float64(count)
		}
	}

	return selected
}

// closingReader wraps a byte slice as an io.ReadCloser
type closingReader struct {
	src []byte
	pos int
}

func (r *closingReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	n := copy(p, r.src[r.pos:])
	r.pos += n
	return n, nil
}

func (r *closingReader) Close() error {
	return nil
}

// readBodyForReason reads up to 200 bytes from body for failure reason logging.
func readBodyForReason(body io.ReadCloser) string {
	respBody, _ := io.ReadAll(io.LimitReader(body, 200))
	body.Close()
	return string(respBody)
}

// truncateString truncates s to at most maxLen bytes.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
