package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CachedStats holds a snapshot of memory stats refreshed in the background.
type CachedStats struct {
	Total       int64
	Error       string
	LastUpdated time.Time
}

// Client is a fire-and-forget HTTP client for the Mem0 memory service.
type Client struct {
	mem0URL    string
	searchURL  string
	httpClient *http.Client
	enabled    bool
	logger     *slog.Logger
	sem        chan struct{} // bounds concurrent RecordAsync calls

	cacheMu   sync.RWMutex
	cached    CachedStats
	stopCh    chan struct{}
}

// Message represents a single message in the Anthropic format.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ConversationRecord holds the data to send to Mem0 after a request completes.
type ConversationRecord struct {
	Messages []Message      `json:"messages"`
	Metadata RecordMetadata `json:"metadata"`
}

// RecordMetadata holds per-request metadata.
type RecordMetadata struct {
	Model     string    `json:"model"`
	Stream    bool      `json:"stream"`
	Duration  int64     `json:"duration_ms"`
	Timestamp time.Time `json:"timestamp"`
}

// NewClient creates a new memory client and starts background stat refresh.
func NewClient(mem0URL, searchURL string, enabled bool, logger *slog.Logger) *Client {
	c := &Client{
		mem0URL:   mem0URL,
		searchURL: searchURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		enabled: enabled,
		logger:  logger,
		sem:     make(chan struct{}, 2), // at most 2 concurrent recordings
		stopCh:  make(chan struct{}),
	}
	if enabled {
		go c.refreshLoop()
	}
	return c
}

// Stop shuts down the background refresh loop.
func (c *Client) Stop() {
	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}
}

func (c *Client) refreshLoop() {
	// Initial fetch
	c.refresh()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.refresh()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Client) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := c.count(ctx)
	c.cacheMu.Lock()
	if err != nil {
		c.cached.Error = err.Error()
		c.logger.Debug("memory: stat refresh failed", "error", err)
	} else {
		c.cached.Total = count
		c.cached.Error = ""
	}
	c.cached.LastUpdated = time.Now()
	c.cacheMu.Unlock()
}

// RecordAsync sends a conversation record to Mem0 in a goroutine.
// Failures are logged and discarded; the caller is never blocked.
// At most cap(sem) concurrent recordings; excess requests are dropped.
func (c *Client) RecordAsync(record ConversationRecord) {
	if !c.enabled {
		return
	}
	select {
	case c.sem <- struct{}{}:
	default:
		c.logger.Debug("memory: dropping record, too many in-flight")
		return
	}
	go func() {
		defer func() { <-c.sem }()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// mem0-aio expects: {"text": "...", "user_id": "...", "infer": true, "app": "..."}
		payload := map[string]interface{}{
			"text":    messagesToText(record.Messages),
			"user_id": "default_user",
			"infer":   true,
			"app":     "claude",
			"metadata": record.Metadata,
		}

		body, err := json.Marshal(payload)
		if err != nil {
			c.logger.Warn("memory: marshal failed", "error", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.mem0URL+"/api/v1/memories/", bytes.NewReader(body))
		if err != nil {
			c.logger.Warn("memory: create request failed", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logger.Warn("memory: post failed (mem0 unreachable)", "error", err)
			return
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			c.logger.Warn("memory: mem0 returned error", "status", resp.StatusCode)
		}
	}()
}

// messagesToText converts a message list into a single text block for mem0 extraction.
func messagesToText(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.Role, m.Content))
	}
	return sb.String()
}

// SearchResult is a single result from a memory search.
type SearchResult struct {
	ID     string  `json:"id"`
	Memory string  `json:"memory"`
	Score  float64 `json:"score"`
}

// Search uses semantic search via the sidecar endpoint.
func (c *Client) Search(query string, limit int, minScore float64) []SearchResult {
	if !c.enabled {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	payload := map[string]interface{}{
		"user_id":   "default_user",
		"query":     query,
		"limit":     limit,
		"threshold": minScore,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.searchURL+"/search", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil
	}

	var results []SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil
	}
	return results
}

// Count returns the cached total number of stored memories.
func (c *Client) Count() (int64, error) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	if c.cached.Error != "" {
		return c.cached.Total, fmt.Errorf(c.cached.Error)
	}
	return c.cached.Total, nil
}

// CountWithContext returns the cached total (context is kept for API compat but unused).
func (c *Client) CountWithContext(_ context.Context) (int64, error) {
	return c.Count()
}

// Stats returns the full cached memory snapshot.
func (c *Client) Stats() CachedStats {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.cached
}

func (c *Client) count(ctx context.Context) (int64, error) {
	if !c.enabled {
		return 0, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.mem0URL+"/api/v1/stats/?user_id=default_user", nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var body struct {
		TotalMemories int `json:"total_memories"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	return int64(body.TotalMemories), nil
}
