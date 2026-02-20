// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"strings"
)

// chatCompletionRequest represents an OpenAI chat completions request.
type chatCompletionRequest struct {
	Messages []chatMessage `json:"messages"`
}

// chatMessage represents a single message in the chat completions format.
type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// chatCompletionResponse represents an OpenAI chat completions response.
type chatCompletionResponse struct {
	Choices []chatChoice `json:"choices"`
}

// chatChoice represents a single choice in the chat completions response.
type chatChoice struct {
	Message *chatChoiceMessage `json:"message"`
}

// chatChoiceMessage represents the message within a choice.
type chatChoiceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// contentPart represents a single part in a multimodal content array.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// chatCompletionsParser implements Parser for the OpenAI Chat Completions format.
type chatCompletionsParser struct{}

func (p *chatCompletionsParser) ParseRequest(body []byte) (string, []string, error) {
	return ParseChatRequest(body)
}

func (p *chatCompletionsParser) ParseResponse(body []byte) (string, error) {
	return ParseChatResponse(body)
}

func (p *chatCompletionsParser) ParseRequestForTaskAdherence(body []byte) (*taskAdherenceRequest, error) {
	return parseChatRequestForTaskAdherence(body)
}

// chatCompletionRequestFull is an extended version of chatCompletionRequest
// that also captures tools and tool_calls for Task Adherence analysis.
type chatCompletionRequestFull struct {
	Messages []chatMessageFull `json:"messages"`
	Tools    []chatTool        `json:"tools"`
}

// chatMessageFull is an extended version of chatMessage that includes tool calls.
type chatMessageFull struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []chatToolCall  `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

// chatTool represents a tool definition in the OpenAI format.
type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

// chatToolFunction represents a function definition within a tool.
type chatToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// chatToolCall represents a tool call in the OpenAI format.
type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

// chatToolCallFunction represents the function details within a tool call.
type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ParseChatRequest parses an OpenAI chat completions request body and extracts
// user prompts and system/document messages for use with Prompt Shield.
func ParseChatRequest(body []byte) (userPrompt string, documents []string, err error) {
	var req chatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, err
	}

	var userParts []string
	for _, msg := range req.Messages {
		text := extractContent(msg.Content)
		switch msg.Role {
		case "user":
			if text != "" {
				userParts = append(userParts, text)
			}
		case "system":
			if text != "" {
				documents = append(documents, text)
			}
		}
	}

	userPrompt = strings.Join(userParts, "\n")
	return userPrompt, documents, nil
}

// ParseChatResponse parses an OpenAI chat completions response body and extracts
// the assistant's content text.
func ParseChatResponse(body []byte) (string, error) {
	var resp chatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	var parts []string
	for _, choice := range resp.Choices {
		if choice.Message != nil && choice.Message.Content != "" {
			parts = append(parts, choice.Message.Content)
		}
	}

	return strings.Join(parts, "\n"), nil
}

// parseChatRequestForTaskAdherence parses an OpenAI chat completions request body
// and translates it into the Azure Task Adherence API format.
func parseChatRequestForTaskAdherence(body []byte) (*taskAdherenceRequest, error) {
	var req chatCompletionRequestFull
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	result := &taskAdherenceRequest{}

	// Translate tools.
	for _, t := range req.Tools {
		result.Tools = append(result.Tools, taskAdherenceTool{
			Type: t.Type,
			Function: taskAdherenceToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
			},
		})
	}

	// Translate messages.
	for _, msg := range req.Messages {
		taMsg := taskAdherenceMessage{
			Role:     titleCase(msg.Role),
			Source:   roleToSource(msg.Role),
			Contents: extractContent(msg.Content),
		}

		// Translate tool_calls.
		for _, tc := range msg.ToolCalls {
			taMsg.ToolCalls = append(taMsg.ToolCalls, taskAdherenceToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: taskAdherenceToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}

		// Translate tool_call_id.
		if msg.ToolCallID != "" {
			taMsg.ToolCallID = msg.ToolCallID
		}

		result.Messages = append(result.Messages, taMsg)
	}

	return result, nil
}

// extractContent handles both string and array content formats.
func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content parts.
	var parts []contentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}
