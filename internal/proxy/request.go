package proxy

import (
	"encoding/json"
	"fmt"
)

// AnthropicRequest represents the structure we need to parse
type AnthropicRequest struct {
	Model string `json:"model"`
}

// ExtractModel extracts the model name from an Anthropic request body
func ExtractModel(body []byte) (string, []byte, error) {
	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", body, fmt.Errorf("parse request: %w", err)
	}

	if req.Model == "" {
		return "", body, fmt.Errorf("missing model field in request")
	}

	return req.Model, body, nil
}

// ReplaceModel creates a new request body with the model field replaced
func ReplaceModel(originalBody []byte, newModel string) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(originalBody, &req); err != nil {
		return nil, fmt.Errorf("parse request for modification: %w", err)
	}

	req["model"] = newModel

	return json.Marshal(req)
}
