package proxy

import (
	"testing"
)

func TestParseRequestModel(t *testing.T) {
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
			model, _, err := ParseRequest([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if model != tt.want {
				t.Errorf("ParseRequest() = %q, want %q", model, tt.want)
			}
		})
	}
}

func TestParseRequestPreservesBody(t *testing.T) {
	original := `{"model": "claude-sonnet-4", "tools": [{"name": "get_weather", "input_schema": {"type": "object"}}]}`

	model, req, err := ParseRequest([]byte(original))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if model != "claude-sonnet-4" {
		t.Errorf("model = %q, want claude-sonnet-4", model)
	}

	if req == nil {
		t.Error("req should not be nil")
	}
}