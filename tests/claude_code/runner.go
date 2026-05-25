package proxytest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// RunResult captures the outcome of running Claude Code as a subprocess.
type RunResult struct {
	ExitCode int           // Process exit code (0 = success)
	Stdout   string        // Captured standard output
	Stderr   string        // Captured standard error
	Duration time.Duration // Time taken for subprocess to complete
}

// ClaudeCodeRunner manages spawning and controlling Claude Code subprocesses.
type ClaudeCodeRunner struct {
	env       map[string]string // Environment variables for Claude Code processes
	cmd       string            // Command to run (default: "claude")
	mu        sync.RWMutex      // Protects process
	process   *os.Process       // Current running process (for Kill method)
}

// NewClaudeCodeRunner creates a new ClaudeCodeRunner with the given environment variables.
// If env is nil, an empty map is created.
func NewClaudeCodeRunner(env map[string]string) *ClaudeCodeRunner {
	if env == nil {
		env = make(map[string]string)
	}
	return &ClaudeCodeRunner{
		env: env,
		cmd: "claude",
	}
}

// newClaudeCodeRunnerWithCommand creates a new ClaudeCodeRunner with a custom command for testing.
func newClaudeCodeRunnerWithCommand(env map[string]string, cmd string) *ClaudeCodeRunner {
	if env == nil {
		env = make(map[string]string)
	}
	return &ClaudeCodeRunner{
		env: env,
		cmd: cmd,
	}
}

// RunWithInput runs Claude Code as a subprocess with the given input on stdin.
// Returns the result including exit code, stdout, stderr, and duration.
func (r *ClaudeCodeRunner) RunWithInput(input string, timeout time.Duration) (*RunResult, error) {
	start := time.Now()

	cmd := exec.Command(r.cmd)

	// Set environment variables
	for key, value := range r.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	// Set up stdin pipe
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	// Capture stdout and stderr
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Store process reference for Kill method
	r.mu.Lock()
	r.process = cmd.Process
	r.mu.Unlock()

	// Ensure process reference is cleaned up when process completes
	defer func() {
		r.mu.Lock()
		r.process = nil
		r.mu.Unlock()
	}()

	// Write input to stdin
	stdin.Write([]byte(input))
	stdin.Close()

	// Wait for process completion with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(timeout):
		cmd.Process.Kill()
		return nil, fmt.Errorf("timeout after %v", timeout)
	case err := <-done:
		duration := time.Since(start)
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		return &RunResult{
			ExitCode: exitCode,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Duration: duration,
		}, nil
	}
}

// Kill terminates the currently running Claude Code process.
// If no process is running, it returns nil.
func (r *ClaudeCodeRunner) Kill() error {
	r.mu.Lock()
	process := r.process
	r.mu.Unlock()

	if process != nil && process.Pid > 0 {
		return process.Kill()
	}
	return nil
}
