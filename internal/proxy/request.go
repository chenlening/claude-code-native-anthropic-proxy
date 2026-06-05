package proxy

import (
	"bytes"
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

// RewriteModel replaces the "model" field value in a JSON request body.
// Uses targeted string replacement to avoid re-serializing the entire body,
// which preserves unknown fields, formatting, and binary-safe content.
func RewriteModel(body []byte, newModel string) ([]byte, error) {
	oldModel, _, err := ParseRequest(body)
	if err != nil {
		return body, nil // can't parse, forward unchanged
	}
	if oldModel == newModel {
		return body, nil // no change needed
	}

	search := `"model":"` + oldModel + `"`
	replacement := `"model":"` + newModel + `"`
	result := bytes.ReplaceAll(body, []byte(search), []byte(replacement))
	if len(result) == len(body) && bytes.Equal(result, body) {
		search2 := `"model": "` + oldModel + `"`
		replacement2 := `"model": "` + newModel + `"`
		result = bytes.ReplaceAll(body, []byte(search2), []byte(replacement2))
	}
	return result, nil
}

// RewriteModelInResponse replaces the top-level "model" field in a non-streaming
// JSON response body with the original frontend model name.
func RewriteModelInResponse(body []byte, frontendModel string) ([]byte, error) {
	var resp struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return body, nil // can't parse, forward unchanged
	}
	if resp.Model == "" || resp.Model == frontendModel {
		return body, nil // no model field or already matches
	}

	search := `"model":"` + resp.Model + `"`
	replacement := `"model":"` + frontendModel + `"`
	result := bytes.ReplaceAll(body, []byte(search), []byte(replacement))
	if len(result) == len(body) && bytes.Equal(result, body) {
		search2 := `"model": "` + resp.Model + `"`
		replacement2 := `"model": "` + frontendModel + `"`
		result = bytes.ReplaceAll(body, []byte(search2), []byte(replacement2))
	}
	return result, nil
}

// RewriteModelInSSE replaces the "model" field inside a streaming SSE data line.
// Only processes message_start events containing a "model" field.
// Returns (rewritten_line, changed). If changed=false, forward the line unchanged.
func RewriteModelInSSE(line []byte, frontendModel string) ([]byte, bool) {
	if !bytes.Contains(line, []byte("message_start")) || !bytes.Contains(line, []byte(`"model"`)) {
		return line, false
	}

	var event struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(line[6:], &event); err != nil { // skip "data: " prefix
		return line, false
	}
	if event.Type != "message_start" {
		return line, false
	}

	var msg struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(event.Message, &msg); err != nil {
		return line, false
	}
	if msg.Model == "" || msg.Model == frontendModel {
		return line, false
	}

	search := `"model":"` + msg.Model + `"`
	replacement := `"model":"` + frontendModel + `"`
	newMsg := bytes.ReplaceAll(event.Message, []byte(search), []byte(replacement))

	rebuilt, _ := json.Marshal(map[string]interface{}{
		"type":    event.Type,
		"message": json.RawMessage(newMsg),
	})
	result := append([]byte("data: "), rebuilt...)
	return result, true
}