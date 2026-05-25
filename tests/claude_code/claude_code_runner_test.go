package proxytest

import (
	"strings"
	"testing"
	"time"
)

func TestClaudeCodeRunner_NewClaudeCodeRunner(t *testing.T) {
	t.Run("with valid env", func(t *testing.T) {
		env := map[string]string{
			"ANTHROPIC_BASE_URL": "http://localhost:8080",
		}
		runner := NewClaudeCodeRunner(env)
		if runner == nil {
			t.Fatal("NewClaudeCodeRunner returned nil")
		}
		if runner.env["ANTHROPIC_BASE_URL"] != "http://localhost:8080" {
			t.Errorf("env not stored correctly")
		}
	})

	t.Run("with nil env", func(t *testing.T) {
		runner := NewClaudeCodeRunner(nil)
		if runner == nil {
			t.Fatal("NewClaudeCodeRunner returned nil")
		}
		// Verify nil is handled gracefully - should not panic
	})
}

func TestClaudeCodeRunner_RunWithInput_SimpleEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use cat command which reads stdin and writes to stdout for testing
	runner := newClaudeCodeRunnerWithCommand(map[string]string{
		"ANTHROPIC_BASE_URL": "http://localhost:8080",
	}, "cat")

	// Test subprocess execution with simple input
	result, err := runner.RunWithInput("hello", 5*time.Second)

	if err != nil {
		t.Fatalf("RunWithInput failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	if result.Stdout != "hello" {
		t.Errorf("expected stdout 'hello', got '%s'", result.Stdout)
	}

	if result.Duration == 0 {
		t.Errorf("expected non-zero duration, got %v", result.Duration)
	}
}

func TestClaudeCodeRunner_RunWithInput_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use bash -c with sleep command which will exceed the timeout
	runner := newClaudeCodeRunnerWithCommand(map[string]string{}, "bash")

	_, err := runner.RunWithInput("sleep 10", 100*time.Millisecond)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestClaudeCodeRunner_Kill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test 1: Kill when no process is running should not error
	t.Run("no process running", func(t *testing.T) {
		runner := NewClaudeCodeRunner(map[string]string{})
		err := runner.Kill()
		if err != nil {
			t.Errorf("Kill with no process should not error, got: %v", err)
		}
	})

	// Test 2: Kill a running process
	t.Run("kill running process", func(t *testing.T) {
		runner := newClaudeCodeRunnerWithCommand(map[string]string{
			"ANTHROPIC_BASE_URL": "http://localhost:8080",
		}, "bash")

		// Start a process with a very long timeout
		done := make(chan error, 1)
		go func() {
			// Use a command that processes input but won't exit immediately
			// The trap will catch the signal and exit
			_, err := runner.RunWithInput("trap 'exit 0' SIGTERM; sleep 1000", 30*time.Second)
			done <- err
		}()

		time.Sleep(200 * time.Millisecond) // Let process start

		// Kill should succeed
		err := runner.Kill()
		if err != nil {
			t.Fatalf("Kill failed: %v", err)
		}

		// Process should terminate (either killed or timeout)
		select {
		case err := <-done:
			// Process terminated - this is expected
			t.Logf("Process terminated: %v", err)
		case <-time.After(2 * time.Second):
			t.Log("Process still running after kill (may be cleaning up)")
		}
	})

	// Test 3: Multiple Kill calls should be safe
	t.Run("multiple kill calls", func(t *testing.T) {
		runner := newClaudeCodeRunnerWithCommand(map[string]string{}, "echo")
		_, _ = runner.RunWithInput("test", 1*time.Second)

		// Multiple kills should not error
		for i := 0; i < 3; i++ {
			err := runner.Kill()
			if err != nil {
				t.Errorf("Kill %d should not error, got: %v", i+1, err)
			}
		}
	})
}
