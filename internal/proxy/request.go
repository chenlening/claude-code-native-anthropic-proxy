package proxy

import (
	"encoding/json"
	"fmt"
)

// AnthropicRequest represents the parsed request body
type AnthropicRequest struct {
	Model    string          `json:"model"`
	Tools    json.RawMessage `json:"tools"`
	Messages json.RawMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

// ParseRequest parses the request body once, returning the model and parsed struct
func ParseRequest(body []byte) (string, *AnthropicRequest, error) {
	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, fmt.Errorf("parse request: %w", err)
	}

	if req.Model == "" {
		return "", nil, fmt.Errorf("missing model field in request")
	}

	return req.Model, &req, nil
}