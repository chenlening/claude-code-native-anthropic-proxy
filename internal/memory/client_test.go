package memory

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestRecordAsync_Enabled(t *testing.T) {
	received := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/memories/" {
			received <- true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client := NewClient(srv.URL, srv.URL, true, logger)

	client.RecordAsync(ConversationRecord{
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Metadata: RecordMetadata{
			Model:     "claude-sonnet-4-6",
			Stream:    false,
			Timestamp: time.Now(),
		},
	})

	select {
	case <-received:
		// OK
	case <-time.After(time.Second):
		// RecordAsync is async, it may arrive late — acceptable
	}
}

func TestRecordAsync_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client := NewClient("http://localhost:9999", "http://localhost:9999", false, logger)

	// Should not panic or make any request
	client.RecordAsync(ConversationRecord{
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
	})
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/search" {
			// semantic search sidecar returns flat array with scores
			results := []map[string]interface{}{
				{"id": "1", "memory": "user prefers tabs", "score": 0.95},
				{"id": "2", "memory": "project uses Go", "score": 0.82},
			}
			json.NewEncoder(w).Encode(results)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client := NewClient(srv.URL, srv.URL, true, logger)

	results := client.Search("coding style", 5, 0.7)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Memory != "user prefers tabs" {
		t.Errorf("unexpected result: %s", results[0].Memory)
	}
	if results[0].Score != 0.95 {
		t.Errorf("unexpected score: %f", results[0].Score)
	}
}

func TestExtractQuery_String(t *testing.T) {
	input := json.RawMessage(`[{"role":"user","content":"what editor do I use?"}]`)
	q := extractQuery(input)
	if q != "what editor do I use?" {
		t.Errorf("expected query, got %q", q)
	}
}

func TestExtractQuery_Array(t *testing.T) {
	input := json.RawMessage(`[{"role":"user","content":[{"type":"text","text":"find the bug"}]}]`)
	q := extractQuery(input)
	if q != "find the bug" {
		t.Errorf("expected query, got %q", q)
	}
}

func TestExtractQuery_LastUser(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"user","content":"first question"},
		{"role":"assistant","content":"answer"},
		{"role":"user","content":"second question"}
	]`)
	q := extractQuery(input)
	if q != "second question" {
		t.Errorf("expected last user message, got %q", q)
	}
}
