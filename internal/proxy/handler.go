package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/metrics"
	"github.com/anthropic-transparent-proxy/internal/routing"
)

// Handler is the main proxy HTTP handler
type Handler struct {
	router    *routing.ModelRouter
	healthMgr *endpoint.HealthManager
	metrics   *metrics.Metrics
	logger    *slog.Logger
}

// NewHandler creates a new proxy handler
func NewHandler(
	router *routing.ModelRouter,
	healthMgr *endpoint.HealthManager,
	metrics *metrics.Metrics,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		router:    router,
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
	frontendModel, _, err := ExtractModel(bodyBytes)
	if err != nil {
		h.logger.Error("failed to extract model", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Resolve pool to determine max attempts
	pool, err := h.router.Resolve(frontendModel)
	if err != nil {
		h.logger.Error("no backend available", "model", frontendModel, "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	maxAttempts := len(pool.Backends)

	// Retry loop for 429 responses
	attempted := make(map[string]bool)

	var resp *http.Response
	var selectedBackend *routing.ModelBackend
	var selectedEp *endpoint.EndpointState

	for attempt := 0; attempt < maxAttempts; attempt++ {
		backend, err := h.router.SelectBackendWithExclusion(frontendModel, attempted)
		if err != nil {
			break // no more backends available
		}
		ep := backend.Endpoint
		attempted[ep.Name] = true

		h.logger.Info("routing request",
			"frontend_model", frontendModel,
			"backend_model", backend.BackendModel,
			"endpoint", ep.Name,
			"attempt", attempt+1,
		)

		// Replace model in request body
		modifiedBody, err := ReplaceModel(bodyBytes, backend.BackendModel)
		if err != nil {
			h.logger.Error("failed to replace model", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Track connection
		ep.IncrementConnection(frontendModel)

		// Create upstream request
		upstreamReq, err := http.NewRequest(r.Method, ep.URL+r.URL.Path, nil)
		if err != nil {
			ep.DecrementConnection(frontendModel)
			h.logger.Error("failed to create upstream request", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Set body from modified content
		upstreamReq.Body = io.NopCloser(io.Reader(nil))
		upstreamReq.ContentLength = int64(len(modifiedBody))
		upstreamReq.Body = &closingReader{src: modifiedBody}

		// Copy headers, replacing auth
		for key, values := range r.Header {
			for _, value := range values {
				lowerKey := key
				// Skip original auth headers
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
			h.metrics.RecordRequest(frontendModel, backend.BackendModel, ep.Name, time.Since(start).Seconds(), false)
			continue
		}

		// If not 429, this is our final response
		if resp.StatusCode != 429 {
			selectedBackend = backend
			selectedEp = ep
			break
		}

		// 429 — close this response, decrement connection, record failure, try next backend
		h.healthMgr.RecordFailure(ep, "429")
		resp.Body.Close()
		ep.DecrementConnection(frontendModel)
		h.logger.Warn("rate limited, retrying on next backend",
			"endpoint", ep.Name, "attempt", attempt+1)
	}

	if resp == nil {
		h.logger.Error("all backends exhausted", "model", frontendModel)
		http.Error(w, "no backend available", http.StatusServiceUnavailable)
		return
	}

	defer resp.Body.Close()
	defer selectedEp.DecrementConnection(frontendModel)

	// Record success/failure based on status code (all non-2xx are failures)
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	if success {
		h.healthMgr.RecordSuccess(selectedEp)
	} else {
		h.healthMgr.RecordFailure(selectedEp, strconv.Itoa(resp.StatusCode))
	}

	h.metrics.RecordRequest(frontendModel, selectedBackend.BackendModel, selectedEp.Name, time.Since(start).Seconds(), success)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Stream response body
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

// closingReader wraps a byte slice as an io.ReadCloser
type closingReader struct {
	src  []byte
	pos  int
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
