package endpoint

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestEndpointStateIncrementConnection(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	if state.GetConnectionCount("claude-sonnet-4") != 0 {
		t.Errorf("initial count = %d, want 0", state.GetConnectionCount("claude-sonnet-4"))
	}

	state.IncrementConnection("claude-sonnet-4")
	if state.GetConnectionCount("claude-sonnet-4") != 1 {
		t.Errorf("after increment = %d, want 1", state.GetConnectionCount("claude-sonnet-4"))
	}

	state.IncrementConnection("claude-sonnet-4")
	if state.GetConnectionCount("claude-sonnet-4") != 2 {
		t.Errorf("after second increment = %d, want 2", state.GetConnectionCount("claude-sonnet-4"))
	}
}

func TestEndpointStateDecrementConnection(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	state.IncrementConnection("claude-sonnet-4")
	state.IncrementConnection("claude-sonnet-4")
	state.DecrementConnection("claude-sonnet-4")

	if state.GetConnectionCount("claude-sonnet-4") != 1 {
		t.Errorf("after decrement = %d, want 1", state.GetConnectionCount("claude-sonnet-4"))
	}
}

func TestEndpointStateConcurrentConnections(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state.IncrementConnection("claude-sonnet-4")
		}()
	}
	wg.Wait()

	if state.GetConnectionCount("claude-sonnet-4") != 100 {
		t.Errorf("concurrent count = %d, want 100", state.GetConnectionCount("claude-sonnet-4"))
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state.DecrementConnection("claude-sonnet-4")
		}()
	}
	wg.Wait()

	if state.GetConnectionCount("claude-sonnet-4") != 50 {
		t.Errorf("after decrements = %d, want 50", state.GetConnectionCount("claude-sonnet-4"))
	}
}

func TestEndpointStateDifferentModels(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	state.IncrementConnection("claude-sonnet-4")
	state.IncrementConnection("claude-opus-4")

	if state.GetConnectionCount("claude-sonnet-4") != 1 {
		t.Errorf("sonnet count = %d, want 1", state.GetConnectionCount("claude-sonnet-4"))
	}
	if state.GetConnectionCount("claude-opus-4") != 1 {
		t.Errorf("opus count = %d, want 1", state.GetConnectionCount("claude-opus-4"))
	}
}

func TestEndpointStateDisabled(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	if state.IsDisabled() {
		t.Error("initial state should not be disabled")
	}

	state.Disable()
	if !state.IsDisabled() {
		t.Error("after Disable() should be disabled")
	}

	state.Enable()
	if state.IsDisabled() {
		t.Error("after Enable() should not be disabled")
	}
}

func TestEndpointStateFailureTracking(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	if state.GetFailures() != 0 {
		t.Errorf("initial failures = %d, want 0", state.GetFailures())
	}

	// Test failure with reason
	state.RecordFailure("429")
	if state.GetFailures() != 1 {
		t.Errorf("after failure = %d, want 1", state.GetFailures())
	}
	if reason := state.GetLastFailureReason(); reason != "429" {
		t.Errorf("lastFailureReason = %q, want 429", reason)
	}
	if state.GetLastFailureTime().IsZero() {
		t.Error("lastFailureTime should be set after failure")
	}

	// Test backward compatible call without reason
	state.RecordFailure()
	if state.GetFailures() != 2 {
		t.Errorf("after second failure = %d, want 2", state.GetFailures())
	}

	state.ResetFailures()
	if state.GetFailures() != 0 {
		t.Errorf("after reset = %d, want 0", state.GetFailures())
	}
}

func TestEndpointStateLastRequestTime(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	if !state.GetLastRequestTime().IsZero() {
		t.Error("initial lastRequestTime should be zero")
	}

	state.IncrementConnection("model-a")
	state.RecordSuccess()

	if state.GetLastRequestTime().IsZero() {
		t.Error("lastRequestTime should be set after RecordSuccess")
	}
}

func TestWithTimeout(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")
	original := state.Client.Timeout

	state.WithTimeout(30 * time.Second)
	if state.Client.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", state.Client.Timeout)
	}

	state.WithTimeout(original)
	if state.Client.Timeout != original {
		t.Errorf("timeout = %v, want %v", state.Client.Timeout, original)
	}
}

func TestGetTotalConnections(t *testing.T) {
	state := NewEndpointState("endpoint-a", "https://api.anthropic.com", "test-key")

	state.IncrementConnection("model-a")
	state.IncrementConnection("model-a")
	state.IncrementConnection("model-b")
	state.IncrementConnection("model-c")

	total := state.GetTotalConnections()
	if total != 4 {
		t.Errorf("total connections = %d, want 4", total)
	}

	state.DecrementConnection("model-a")
	total = state.GetTotalConnections()
	if total != 3 {
		t.Errorf("total after decrement = %d, want 3", total)
	}
}

func TestVerify(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		state := NewEndpointState("test", server.URL, "test-key")
		if err := state.Verify("claude-sonnet-4"); err != nil {
			t.Errorf("Verify() error = %v", err)
		}
	})

	t.Run("auth error not acceptable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		state := NewEndpointState("test", server.URL, "test-key")
		if err := state.Verify("claude-sonnet-4"); err == nil {
			t.Error("Verify() should fail for 401")
		}
	})

	t.Run("rate limit not acceptable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		state := NewEndpointState("test", server.URL, "test-key")
		if err := state.Verify("claude-sonnet-4"); err == nil {
			t.Error("Verify() should fail for 429")
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		state := NewEndpointState("test", server.URL, "test-key")
		if err := state.Verify("claude-sonnet-4"); err == nil {
			t.Error("Verify() should fail for 500")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		state := NewEndpointState("test", server.URL, "test-key")
		state.WithTimeout(100 * time.Millisecond)
		if err := state.Verify("claude-sonnet-4"); err == nil {
			t.Error("Verify() should timeout")
		}
	})
}
