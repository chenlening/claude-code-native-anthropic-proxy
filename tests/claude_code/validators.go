package proxytest

import (
	"fmt"
	"strings"
)

// ValidateBasicChat validates that a Claude Code subprocess completed successfully
// and produced non-empty output. Returns an error if exit code is non-zero or stdout is empty.
func ValidateBasicChat(result *RunResult) error {
	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("unexpected exit code %d", result.ExitCode)
	}

	if strings.TrimSpace(result.Stdout) == "" {
		return fmt.Errorf("empty stdout")
	}

	return nil
}

// ValidateToolUse validates that tool usage patterns are present in the output.
// Returns an error if exit code is non-zero or no tool patterns are found.
func ValidateToolUse(result *RunResult) error {
	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("unexpected exit code %d", result.ExitCode)
	}

	// Check for various tool use indicators
	hasToolKeyword := strings.Contains(strings.ToLower(result.Stdout), "tool")
	hasCommandExecution := strings.Contains(strings.ToLower(result.Stdout), "command") ||
		strings.Contains(strings.ToLower(result.Stdout), "executed")
	hasFileListings := strings.Contains(result.Stdout, ".md") ||
		strings.Contains(result.Stdout, ".go") ||
		strings.Contains(result.Stdout, ".yaml") ||
		strings.Contains(result.Stdout, "Makefile")

	if !hasToolKeyword && !hasCommandExecution && !hasFileListings {
		return fmt.Errorf("no tool usage patterns found in output")
	}

	return nil
}

// ValidateExtendedThinking validates that thinking blocks are present in the output.
// Returns an error if exit code is non-zero or no <thinking> blocks are found.
func ValidateExtendedThinking(result *RunResult) error {
	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("unexpected exit code %d", result.ExitCode)
	}

	if !strings.Contains(result.Stdout, "<thinking>") {
		return fmt.Errorf("no thinking blocks found in output")
	}

	return nil
}
