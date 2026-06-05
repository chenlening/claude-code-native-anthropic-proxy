package proxy

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRewriteModel(t *testing.T) {
	body := `{"model":"glm-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	result, err := RewriteModel([]byte(body), "GLM-5")
	if err != nil {
		t.Fatalf("RewriteModel error: %v", err)
	}
	model, _, parseErr := ParseRequest(result)
	if parseErr != nil {
		t.Fatalf("ParseRequest error: %v", parseErr)
	}
	if model != "GLM-5" {
		t.Errorf("model = %q, want GLM-5", model)
	}
}

func TestRewriteModelNoChange(t *testing.T) {
	body := `{"model":"glm-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	result, err := RewriteModel([]byte(body), "glm-5")
	if err != nil {
		t.Fatalf("RewriteModel error: %v", err)
	}
	model, _, parseErr := ParseRequest(result)
	if parseErr != nil {
		t.Fatalf("ParseRequest error: %v", parseErr)
	}
	if model != "glm-5" {
		t.Errorf("model = %q, want glm-5 (unchanged)", model)
	}
}

func TestRewriteModelInResponse(t *testing.T) {
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"GLM-5","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn","usage":{"input_tokens":10}}`
	result, err := RewriteModelInResponse([]byte(body), "glm-5")
	if err != nil {
		t.Fatalf("RewriteModelInResponse error: %v", err)
	}
	var resp map[string]interface{}
	if jsonErr := json.Unmarshal(result, &resp); jsonErr != nil {
		t.Fatalf("json.Unmarshal error: %v", jsonErr)
	}
	if resp["model"] != "glm-5" {
		t.Errorf("model = %v, want glm-5", resp["model"])
	}
}

func TestRewriteModelInResponseNoModelField(t *testing.T) {
	body := `{"id":"msg_1","type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`
	result, err := RewriteModelInResponse([]byte(body), "glm-5")
	if err != nil {
		t.Fatalf("RewriteModelInResponse error: %v", err)
	}
	if string(result) != body {
		t.Errorf("expected unchanged body when no model field")
	}
}

func TestRewriteModelInResponseAlreadyMatches(t *testing.T) {
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"glm-5","content":[{"type":"text","text":"pong"}]}`
	result, err := RewriteModelInResponse([]byte(body), "glm-5")
	if err != nil {
		t.Fatalf("RewriteModelInResponse error: %v", err)
	}
	if string(result) != body {
		t.Errorf("expected unchanged body when model already matches")
	}
}

func TestRewriteModelInSSE(t *testing.T) {
	line := `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"GLM-5","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`
	result, changed := RewriteModelInSSE([]byte(line), "glm-5")
	if !changed {
		t.Errorf("expected changed=true for message_start with model field")
	}
	if !bytes.Contains(result, []byte(`"model":"glm-5"`)) {
		t.Errorf("result should contain model=glm-5, got: %s", result)
	}
	if bytes.Contains(result, []byte(`"model":"GLM-5"`)) {
		t.Errorf("result should not contain model=GLM-5")
	}
}

func TestRewriteModelInSSEOtherEvent(t *testing.T) {
	line := `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}`
	result, changed := RewriteModelInSSE([]byte(line), "glm-5")
	if changed {
		t.Errorf("expected changed=false for non-message_start event")
	}
	if !bytes.Equal(result, []byte(line)) {
		t.Errorf("expected unchanged line for non-message_start event")
	}
}

func TestRewriteModelInSSEAlreadyMatches(t *testing.T) {
	line := `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"glm-5","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`
	result, changed := RewriteModelInSSE([]byte(line), "glm-5")
	if changed {
		t.Errorf("expected changed=false when model already matches frontend name")
	}
	if !bytes.Equal(result, []byte(line)) {
		t.Errorf("expected unchanged line when model already matches")
	}
}