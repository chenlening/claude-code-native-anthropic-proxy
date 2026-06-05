package memory

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Injector builds context injection text from Mem0 search results.
type Injector struct {
	client        *Client
	maxMemories   int
	minConfidence float64
	enabled       bool
}

// NewInjector creates a new context injector.
func NewInjector(client *Client, maxMemories int, minConfidence float64, enabled bool) *Injector {
	return &Injector{
		client:        client,
		maxMemories:   maxMemories,
		minConfidence: minConfidence,
		enabled:       enabled,
	}
}

// BuildContext searches Mem0 for relevant memories and formats them as a text block.
// Returns empty string if injection is disabled or no results found.
func (inj *Injector) BuildContext(messagesJSON json.RawMessage) string {
	if !inj.enabled {
		return ""
	}

	query := extractQuery(messagesJSON)
	if query == "" {
		return ""
	}

	results := inj.client.Search(query, inj.maxMemories, inj.minConfidence)
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n<personal_memory>\n")
	sb.WriteString("The following are relevant facts from past interactions with this user:\n\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("- %s (confidence: %.2f)\n", r.Memory, r.Score))
	}
	sb.WriteString("</personal_memory>")

	return sb.String()
}

// extractQuery pulls the last user message content for use as a search query.
func extractQuery(messagesJSON json.RawMessage) string {
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messagesJSON, &messages); err != nil {
		return ""
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			var content string
			// Content can be a string or an array of content blocks
			if err := json.Unmarshal(messages[i].Content, &content); err == nil {
				return content
			}
			// Try array format
			var blocks []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(messages[i].Content, &blocks); err == nil {
				var parts []string
				for _, b := range blocks {
					if b.Text != "" {
						parts = append(parts, b.Text)
					}
				}
				return strings.Join(parts, " ")
			}
		}
	}
	return ""
}
