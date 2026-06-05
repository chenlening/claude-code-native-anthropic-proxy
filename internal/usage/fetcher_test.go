package usage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockProvider is a test provider
type mockProvider struct {
	name  string
	data  *UsageData
	err   error
	calls int
	mu    sync.Mutex
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) FetchUsage(ctx context.Context, baseURL, apiKey string) (*UsageData, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.err != nil {
		return &UsageData{Provider: m.name, Error: m.err.Error(), FetchedAt: time.Now()}, m.err
	}
	return m.data, nil
}

func (m *mockProvider) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestFetcherDetectProvider(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://open.bigmodel.cn/api/anthropic", "zhipu"},
		{"https://api.deepseek.com/anthropic", "deepseek"},
		{"https://api.minimaxi.com/anthropic", ""},
		{"https://unknown.example.com/api", ""},
	}
	for _, tt := range tests {
		p := detectProvider(tt.url, "", "")
		name := ""
		if p != nil {
			name = p.Name()
		}
		if name != tt.expected {
			t.Errorf("detectProvider(%q) = %q, want %q", tt.url, name, tt.expected)
		}
	}
}

func TestFetcherGetAll(t *testing.T) {
	mp := &mockProvider{
		name: "mock",
		data: &UsageData{
			Provider:  "mock",
			Summary:   "50%",
			FetchedAt: time.Now(),
		},
	}

	f := NewFetcher(map[string]Provider{"test-endpoint": mp}, map[string]EPConfig{"test-endpoint": {"http://localhost", "key"}})
	f.fetchAll(context.Background())

	data := f.GetAll()
	if len(data) != 1 {
		t.Fatalf("GetAll count = %d, want 1", len(data))
	}
	if data["test-endpoint"].Summary != "50%" {
		t.Errorf("Summary = %q, want 50%%", data["test-endpoint"].Summary)
	}
}

func TestFetcherHandlesError(t *testing.T) {
	mp := &mockProvider{
		name: "mock",
		err:  fmt.Errorf("connection refused"),
	}

	f := NewFetcher(map[string]Provider{"ep": mp}, map[string]EPConfig{"ep": {"http://localhost", "key"}})
	f.fetchAll(context.Background())

	data := f.GetAll()
	if data["ep"].Error == "" {
		t.Error("expected error in UsageData")
	}
}

func TestFetcherFromConfig(t *testing.T) {
	endpoints := map[string]EPInput{
		"zhipu":     {URL: "https://open.bigmodel.cn/api/paas/v4", APIKey: "zp-key"},
		"deepseek":  {URL: "https://api.deepseek.com", APIKey: "ds-key"},
		"unknown":   {URL: "https://unknown.example.com", APIKey: "uk-key"},
	}

	f := NewFetcherFromConfig(endpoints)
	if len(f.providers) != 2 {
		t.Errorf("providers count = %d, want 2", len(f.providers))
	}
	if _, ok := f.providers["zhipu"]; !ok {
		t.Error("expected zhipu provider")
	}
	if _, ok := f.providers["deepseek"]; !ok {
		t.Error("expected deepseek provider")
	}
	if _, ok := f.providers["unknown"]; ok {
		t.Error("unknown provider should not be detected")
	}
}

func TestFetcherStops(t *testing.T) {
	mp := &mockProvider{
		name: "mock",
		data: &UsageData{Provider: "mock", Summary: "ok", FetchedAt: time.Now()},
	}
	f := NewFetcher(map[string]Provider{"ep": mp}, map[string]EPConfig{"ep": {"http://localhost", "key"}})

	done := make(chan struct{})
	go func() {
		f.Run(50 * time.Millisecond)
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	f.Stop()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Fetcher.Run did not stop after Stop()")
	}
}
