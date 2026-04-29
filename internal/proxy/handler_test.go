package proxy

import (
	"encoding/json"
	"testing"
)

func TestExtractModelFromRequest(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name:    "valid model field",
			body:    `{"model": "claude-sonnet-4-20250514", "messages": [{"role": "user", "content": "Hello"}]}`,
			want:    "claude-sonnet-4-20250514",
			wantErr: false,
		},
		{
			name:    "model with other fields",
			body:    `{"max_tokens": 1024, "model": "claude-opus-4", "system": "You are helpful", "messages": []}`,
			want:    "claude-opus-4",
			wantErr: false,
		},
		{
			name:    "missing model field",
			body:    `{"messages": [{"role": "user", "content": "Hello"}]}`,
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty model field",
			body:    `{"model": "", "messages": []}`,
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			body:    `{invalid json}`,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, _, err := ExtractModel([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractModel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if model != tt.want {
				t.Errorf("ExtractModel() = %q, want %q", model, tt.want)
			}
		})
	}
}

func TestReplaceModelInRequest(t *testing.T) {
	original := `{"model": "claude-sonnet-4", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello"}]}`

	modified, err := ReplaceModel([]byte(original), "custom-sonnet-model")
	if err != nil {
		t.Fatalf("ReplaceModel() error = %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(modified, &req); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if req["model"] != "custom-sonnet-model" {
		t.Errorf("model = %v, want custom-sonnet-model", req["model"])
	}

	// JSON numbers are float64 in Go
	maxTokens, ok := req["max_tokens"].(float64)
	if !ok || maxTokens != 1024 {
		t.Errorf("max_tokens = %v, want 1024", req["max_tokens"])
	}
}

func TestExtractModelPreservesBody(t *testing.T) {
	original := `{"model": "claude-sonnet-4", "tools": [{"name": "get_weather", "input_schema": {"type": "object"}}]}`

	model, bodyBytes, err := ExtractModel([]byte(original))
	if err != nil {
		t.Fatalf("ExtractModel() error = %v", err)
	}

	if model != "claude-sonnet-4" {
		t.Errorf("model = %q, want claude-sonnet-4", model)
	}

	if len(bodyBytes) == 0 {
		t.Error("bodyBytes should not be empty")
	}
}
