// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"strings"
)

// anthropicParser implements Parser for the Anthropic Messages API format.
type anthropicParser struct{}

// anthropicRequest represents an Anthropic Messages API request.
type anthropicRequest struct {
	System   json.RawMessage    `json:"system"`
	Messages []anthropicMessage `json:"messages"`
	Tools    []anthropicTool    `json:"tools"`
}

// anthropicMessage represents a single message in the Anthropic format.
type anthropicMessage struct {
	Role    string          `json:"role"` // "user", "assistant"
	Content json.RawMessage `json:"content"`
}

// anthropicContentBlock represents a content block in the Anthropic format.
type anthropicContentBlock struct {
	Type      string          `json:"type"`                  // "text", "tool_use", "tool_result"
	Text      string          `json:"text,omitempty"`        // text block
	ID        string          `json:"id,omitempty"`          // tool_use
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result (string or blocks)
}

// anthropicTool represents a tool definition in the Anthropic format.
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicResponse represents an Anthropic Messages API response.
type anthropicResponse struct {
	Type    string                  `json:"type"` // "message"
	Role    string                  `json:"role"` // "assistant"
	Content []anthropicContentBlock `json:"content"`
}

// extractAnthropicSystem extracts text from the top-level "system" field,
// which can be either a string or an array of content blocks.
func extractAnthropicSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content blocks.
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var texts []string
		for i := range blocks {
			if blocks[i].Type == "text" && blocks[i].Text != "" {
				texts = append(texts, blocks[i].Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

// extractAnthropicContent extracts text content from an Anthropic message's
// content field, which can be a string or an array of content blocks.
func extractAnthropicContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content blocks — collect only text blocks.
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var texts []string
		for i := range blocks {
			if blocks[i].Type == "text" && blocks[i].Text != "" {
				texts = append(texts, blocks[i].Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

func (p *anthropicParser) ParseRequest(body []byte) (string, []string, error) {
	var req anthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, err
	}

	// Top-level system field becomes a document.
	var documents []string
	if sysText := extractAnthropicSystem(req.System); sysText != "" {
		documents = append(documents, sysText)
	}

	// Extract user message content.
	var userParts []string
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			if text := extractAnthropicContent(msg.Content); text != "" {
				userParts = append(userParts, text)
			}
		}
	}

	return strings.Join(userParts, "\n"), documents, nil
}

func (p *anthropicParser) ParseResponse(body []byte) (string, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	var parts []string
	for i := range resp.Content {
		if resp.Content[i].Type == "text" && resp.Content[i].Text != "" {
			parts = append(parts, resp.Content[i].Text)
		}
	}

	return strings.Join(parts, "\n"), nil
}

func (p *anthropicParser) ParseRequestForTaskAdherence(body []byte) (*taskAdherenceRequest, error) {
	var req anthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	result := &taskAdherenceRequest{}

	// Translate tools.
	for _, t := range req.Tools {
		result.Tools = append(result.Tools, taskAdherenceTool{
			Type: "function",
			Function: taskAdherenceToolFunction{
				Name:        t.Name,
				Description: t.Description,
			},
		})
	}

	// Add system message if present.
	if sysText := extractAnthropicSystem(req.System); sysText != "" {
		result.Messages = append(result.Messages, taskAdherenceMessage{
			Role:     "System",
			Source:   "Prompt",
			Contents: sysText,
		})
	}

	// Translate messages.
	for _, msg := range req.Messages {
		// Parse content blocks to check for tool_use and tool_result.
		var blocks []anthropicContentBlock
		_ = json.Unmarshal(msg.Content, &blocks)

		// Collect text content for the main message.
		textContent := extractAnthropicContent(msg.Content)

		taMsg := taskAdherenceMessage{
			Role:     titleCase(msg.Role),
			Source:   roleToSource(msg.Role),
			Contents: textContent,
		}

		// Extract tool_use blocks from assistant messages as tool calls.
		if msg.Role == "assistant" {
			for i := range blocks {
				if blocks[i].Type == "tool_use" {
					args := ""
					if len(blocks[i].Input) > 0 {
						args = string(blocks[i].Input)
					}
					taMsg.ToolCalls = append(taMsg.ToolCalls, taskAdherenceToolCall{
						ID:   blocks[i].ID,
						Type: "function",
						Function: taskAdherenceToolCallFunction{
							Name:      blocks[i].Name,
							Arguments: args,
						},
					})
				}
			}
		}

		result.Messages = append(result.Messages, taMsg)

		// Extract tool_result blocks from user messages as separate tool messages.
		if msg.Role == "user" {
			for i := range blocks {
				if blocks[i].Type == "tool_result" {
					toolContent := extractAnthropicContent(blocks[i].Content)
					result.Messages = append(result.Messages, taskAdherenceMessage{
						Role:       "Tool",
						Source:     "Completion",
						Contents:   toolContent,
						ToolCallID: blocks[i].ToolUseID,
					})
				}
			}
		}
	}

	return result, nil
}
