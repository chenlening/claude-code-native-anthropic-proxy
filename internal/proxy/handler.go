package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/memory"
	"github.com/anthropic-transparent-proxy/internal/metrics"
)

// Handler is the main proxy HTTP handler
type Handler struct {
	healthMgr         *endpoint.HealthManager
	metrics           *metrics.Metrics
	memoryClient      *memory.Client
	usageFetcher      usageFetcher
	usageVirtualConns float64
	logger            *slog.Logger
}

// NewHandler creates a new proxy handler
func NewHandler(
	healthMgr *endpoint.HealthManager,
	metrics *metrics.Metrics,
	memoryClient *memory.Client,
	usageFetcher usageFetcher,
	usageVirtualConns float64,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		healthMgr:         healthMgr,
		metrics:           metrics,
		memoryClient:      memoryClient,
		usageFetcher:      usageFetcher,
		usageVirtualConns: usageVirtualConns,
		logger:            logger,
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

	filterFallbackEndpoints(supportedEndpoints)

	maxAttempts := len(supportedEndpoints)

	// Retry loop for 429 responses
	attempted := make(map[string]bool)

	var resp *http.Response
	var selectedEp *endpoint.EndpointState

	for attempt := 0; attempt < maxAttempts; attempt++ {
		ep := h.selectEndpoint(frontendModel, supportedEndpoints, attempted)
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

		// Resolve backend model name and rewrite if needed
		backendModel := ep.BackendModelFor(frontendModel)
		forwardBody := bodyBytes
		if backendModel != frontendModel {
			var rewriteErr error
			forwardBody, rewriteErr = RewriteModel(bodyBytes, backendModel)
			if rewriteErr != nil {
				h.logger.Warn("failed to rewrite model, forwarding unchanged", "error", rewriteErr)
				forwardBody = bodyBytes
			}
		}

		// Create upstream request
		upstreamReq, err := http.NewRequest(r.Method, ep.URL+r.URL.Path, nil)
		if err != nil {
			ep.DecrementConnection(frontendModel)
			h.logger.Error("failed to create upstream request", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Propagate client context so upstream is cancelled if client disconnects
		upstreamReq = upstreamReq.WithContext(r.Context())

		upstreamReq.ContentLength = int64(len(forwardBody))
		upstreamReq.Body = &closingReader{src: forwardBody}

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

	// Compute backendModel for the selected endpoint (needed for reverse rewriting)
	backendModel := selectedEp.BackendModelFor(frontendModel)
	h.logger.Debug("selected endpoint", "endpoint", selectedEp.Name, "frontend_model", frontendModel, "backend_model", backendModel)

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
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()

			// Rewrite model in message_start events if needed
			if backendModel != frontendModel && bytes.HasPrefix(line, []byte("data: ")) {
				rewritten, changed := RewriteModelInSSE(line, frontendModel)
				if changed {
					line = rewritten
				}
			}

			w.Write(line)
			w.Write([]byte("\n"))
			if canFlush {
				flusher.Flush()
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

		// Reverse rewrite: restore frontend model name in response
		if backendModel != frontendModel {
			body, err = RewriteModelInResponse(body, frontendModel)
			if err != nil {
				h.logger.Warn("failed to rewrite model in response, forwarding unchanged", "error", err)
			}
		}

		w.Write(body)
	}

	// Record conversation to memory (fire-and-forget)
	h.recordConversation(parsedReq, bodyBytes, start)
}

// selectLeastConnectionsEndpoint selects the endpoint with the lowest connection count
// recordConversation sends the conversation to Mem0 for memory recording.
func (h *Handler) recordConversation(parsedReq *AnthropicRequest, reqBody []byte, start time.Time) {
	if h.memoryClient == nil {
		return
	}

	var wrapper struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(reqBody, &wrapper); err != nil {
		return
	}

	// Only keep the last 4 messages (2 turns) to stay within embedding limits.
	msgs := wrapper.Messages
	if len(msgs) > 4 {
		msgs = msgs[len(msgs)-4:]
	}
	memMessages := make([]memory.Message, 0, len(msgs))
	for _, m := range msgs {
		contentStr, _ := json.Marshal(m.Content)
		memMessages = append(memMessages, memory.Message{
			Role:    m.Role,
			Content: string(contentStr),
		})
	}

	h.memoryClient.RecordAsync(memory.ConversationRecord{
		Messages: memMessages,
		Metadata: memory.RecordMetadata{
			Model:     parsedReq.Model,
			Stream:    parsedReq.Stream,
			Duration:  time.Since(start).Milliseconds(),
			Timestamp: time.Now(),
		},
	})
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
