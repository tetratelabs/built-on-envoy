// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"strings"
)

// responsesParser implements Parser for the OpenAI Responses API format.
type responsesParser struct{}

// responsesRequest represents an OpenAI Responses API request.
type responsesRequest struct {
	Input json.RawMessage `json:"input"`
	Tools []responsesTool `json:"tools"`
}

// responsesInputItem represents a single item in the Responses API input array.
type responsesInputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or [{type:"input_text",text}]
	// Function call fields (for output items included in input).
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// responsesContentPart represents a content part in the Responses API.
type responsesContentPart struct {
	Type string `json:"type"` // "input_text", "output_text", etc.
	Text string `json:"text"`
}

// responsesTool represents a tool definition in the Responses API.
type responsesTool struct {
	Type        string `json:"type"` // "function", "web_search", etc.
	Name        string `json:"name"`
	Description string `json:"description"`
}

// responsesResponse represents an OpenAI Responses API response.
type responsesResponse struct {
	Output []responsesOutputItem `json:"output"`
}

// responsesOutputItem represents a single item in the Responses API output.
type responsesOutputItem struct {
	Type      string                 `json:"type"` // "message", "function_call", etc.
	Content   []responsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
}

// extractResponsesContent extracts text from a Responses API content field.
// Handles both string and [{type:"input_text",text}] formats.
func extractResponsesContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content parts (input_text, output_text, text).
	var parts []responsesContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

func (p *responsesParser) ParseRequest(body []byte) (string, []string, error) {
	var req responsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, err
	}

	// Input can be a simple string prompt.
	var simpleInput string
	if err := json.Unmarshal(req.Input, &simpleInput); err == nil {
		return simpleInput, nil, nil
	}

	// Or an array of input items.
	var items []responsesInputItem
	if err := json.Unmarshal(req.Input, &items); err != nil {
		return "", nil, err
	}

	var userParts []string
	var documents []string
	for _, item := range items {
		text := extractResponsesContent(item.Content)
		switch item.Role {
		case "user":
			if text != "" {
				userParts = append(userParts, text)
			}
		case "developer":
			if text != "" {
				documents = append(documents, text)
			}
		}
	}

	return strings.Join(userParts, "\n"), documents, nil
}

func (p *responsesParser) ParseResponse(body []byte) (string, error) {
	var resp responsesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	var parts []string
	for _, item := range resp.Output {
		if item.Type == "message" {
			for _, cp := range item.Content {
				if cp.Text != "" {
					parts = append(parts, cp.Text)
				}
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

func (p *responsesParser) ParseRequestForTaskAdherence(body []byte) (*taskAdherenceRequest, error) {
	var req responsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	result := &taskAdherenceRequest{}

	// Translate tools.
	for _, t := range req.Tools {
		if t.Type == "function" {
			result.Tools = append(result.Tools, taskAdherenceTool{
				Type: "function",
				Function: taskAdherenceToolFunction{
					Name:        t.Name,
					Description: t.Description,
				},
			})
		}
	}

	// Input might be a simple string — wrap as a single user message.
	var simpleInput string
	if err := json.Unmarshal(req.Input, &simpleInput); err == nil {
		result.Messages = append(result.Messages, taskAdherenceMessage{
			Role:     "User",
			Source:   "Prompt",
			Contents: simpleInput,
		})
		return result, nil
	}

	// Array of input items.
	var items []responsesInputItem
	if err := json.Unmarshal(req.Input, &items); err != nil {
		return nil, err
	}

	for _, item := range items {
		switch item.Type {
		case "function_call":
			// Function call output items represent assistant tool calls.
			result.Messages = append(result.Messages, taskAdherenceMessage{
				Role:   "Assistant",
				Source: "Completion",
				ToolCalls: []taskAdherenceToolCall{{
					ID:   item.CallID,
					Type: "function",
					Function: taskAdherenceToolCallFunction{
						Name:      item.Name,
						Arguments: item.Arguments,
					},
				}},
			})
		case "function_call_output":
			// Tool output items.
			result.Messages = append(result.Messages, taskAdherenceMessage{
				Role:       "Tool",
				Source:     "Completion",
				Contents:   item.Output,
				ToolCallID: item.CallID,
			})
		default:
			// Message items (user, developer, assistant).
			role := item.Role
			if role == "" {
				continue
			}
			result.Messages = append(result.Messages, taskAdherenceMessage{
				Role:     titleCase(role),
				Source:   roleToSource(role),
				Contents: extractResponsesContent(item.Content),
			})
		}
	}

	return result, nil
}
