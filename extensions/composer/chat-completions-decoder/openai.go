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
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools"`
}

// chatMessage represents a single message in the chat completions format.
type chatMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls []chatToolCall  `json:"tool_calls"`
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

// chatToolCall represents a tool call made by the assistant.
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

// contentPart represents a single part in a multimodal content array.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// chatCompletionResponse represents an OpenAI chat completions response.
type chatCompletionResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
}

// chatChoice represents a single choice in a chat completion response.
type chatChoice struct {
	Index   int         `json:"index"`
	Message chatMessage `json:"message"`
}

// chatUsage represents token usage information in a chat completion response.
type chatUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details"`
}

// completionTokensDetails represents the breakdown of completion token usage.
type completionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
	AudioTokens     int `json:"audio_tokens"`
}

// decodedRequest holds the structured information extracted from a ChatCompletion request.
type decodedRequest struct {
	Model    string
	Messages []chatMessage
	Tools    []chatTool
}

// decodedResponse holds the structured information extracted from a ChatCompletion response.
type decodedResponse struct {
	Choices []chatChoice
	Usage   *chatUsage
}

// decodeChatRequest parses an OpenAI chat completions request body and extracts
// structured information for use in filter metadata.
func decodeChatRequest(body []byte) (*decodedRequest, error) {
	var req chatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &decodedRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Tools:    req.Tools,
	}, nil
}

// decodeChatResponse parses an OpenAI chat completions response body and extracts
// structured information for use in filter metadata.
func decodeChatResponse(body []byte) (*decodedResponse, error) {
	var resp chatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &decodedResponse{
		Choices: resp.Choices,
		Usage:   resp.Usage,
	}, nil
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
