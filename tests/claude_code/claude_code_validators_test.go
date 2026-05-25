package proxytest

import (
	"testing"
)

func TestValidateBasicChat(t *testing.T) {
	result := &RunResult{
		ExitCode: 0,
		Stdout:   "Response: hello",
		Stderr:   "",
	}

	err := ValidateBasicChat(result)
	if err != nil {
		t.Errorf("ValidateBasicChat failed: %v", err)
	}
}

func TestValidateBasicChat_NilResult(t *testing.T) {
	err := ValidateBasicChat(nil)
	if err == nil {
		t.Error("expected error for nil result")
	}
	if err.Error() != "result cannot be nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateBasicChat_NonZeroExitCode(t *testing.T) {
	result := &RunResult{
		ExitCode: 1,
		Stdout:   "some output",
		Stderr:   "error message",
	}
	err := ValidateBasicChat(result)
	if err == nil {
		t.Error("expected error for non-zero exit code")
	}
}

func TestValidateBasicChat_EmptyStdout(t *testing.T) {
	result := &RunResult{
		ExitCode: 0,
		Stdout:   "",
		Stderr:   "",
	}
	err := ValidateBasicChat(result)
	if err == nil {
		t.Error("expected error for empty stdout")
	}
}

func TestValidateToolUse(t *testing.T) {
	result := &RunResult{
		ExitCode: 0,
		Stdout:   "Tool: bash\nCommand: ls",
		Stderr:   "",
	}

	err := ValidateToolUse(result)
	if err != nil {
		t.Errorf("ValidateToolUse failed: %v", err)
	}
}

func TestValidateToolUse_NilResult(t *testing.T) {
	err := ValidateToolUse(nil)
	if err == nil {
		t.Error("expected error for nil result")
	}
	if err.Error() != "result cannot be nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateToolUse_NonZeroExitCode(t *testing.T) {
	result := &RunResult{
		ExitCode: 1,
		Stdout:   "Tool: bash",
		Stderr:   "error message",
	}
	err := ValidateToolUse(result)
	if err == nil {
		t.Error("expected error for non-zero exit code")
	}
}

func TestValidateToolUse_NoToolPatterns(t *testing.T) {
	result := &RunResult{
		ExitCode: 0,
		Stdout:   "just regular text here",
		Stderr:   "",
	}
	err := ValidateToolUse(result)
	if err == nil {
		t.Error("expected error for missing tool patterns")
	}
}

func TestValidateExtendedThinking(t *testing.T) {
	result := &RunResult{
		ExitCode: 0,
		Stdout:   "<thinking>\nLet me think...\n</thinking>\n4",
		Stderr:   "",
	}

	err := ValidateExtendedThinking(result)
	if err != nil {
		t.Errorf("ValidateExtendedThinking failed: %v", err)
	}
}

func TestValidateExtendedThinking_NilResult(t *testing.T) {
	err := ValidateExtendedThinking(nil)
	if err == nil {
		t.Error("expected error for nil result")
	}
	if err.Error() != "result cannot be nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateExtendedThinking_NonZeroExitCode(t *testing.T) {
	result := &RunResult{
		ExitCode: 1,
		Stdout:   "<thinking>thinking</thinking>",
		Stderr:   "error message",
	}
	err := ValidateExtendedThinking(result)
	if err == nil {
		t.Error("expected error for non-zero exit code")
	}
}

func TestValidateExtendedThinking_NoThinkingBlocks(t *testing.T) {
	result := &RunResult{
		ExitCode: 0,
		Stdout:   "just regular text without thinking",
		Stderr:   "",
	}
	err := ValidateExtendedThinking(result)
	if err == nil {
		t.Error("expected error for missing thinking blocks")
	}
}
