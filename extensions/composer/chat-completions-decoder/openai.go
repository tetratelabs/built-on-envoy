// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

type (
	// chatCompletionRequest represents an OpenAI chat completions request.
	chatCompletionRequest struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Tools    []chatTool    `json:"tools"`
	}

	// chatMessage represents a single message in the chat completions format.
	chatMessage struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []chatToolCall  `json:"tool_calls"`
	}

	// chatTool represents a tool definition in the OpenAI format.
	chatTool struct {
		Type     string           `json:"type"`
		Function chatToolFunction `json:"function"`
	}

	// chatToolFunction represents a function definition within a tool.
	chatToolFunction struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	// chatToolCall represents a tool call made by the assistant.
	chatToolCall struct {
		ID       string               `json:"id"`
		Type     string               `json:"type"`
		Function chatToolCallFunction `json:"function"`
	}

	// chatToolCallFunction represents the function details within a tool call.
	chatToolCallFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}

	// contentPart represents a single part in a multimodal content array.
	contentPart struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}

	// chatCompletionResponse represents an OpenAI chat completions response.
	chatCompletionResponse struct {
		Choices []chatChoice `json:"choices"`
		Usage   *chatUsage   `json:"usage"`
	}

	// chatCompletionChunk represents a single chunk in a streaming SSE response.
	chatCompletionChunk struct {
		Object  string            `json:"object"`
		Choices []chatChunkChoice `json:"choices"`
		Usage   *chatUsage        `json:"usage"`
	}

	// chatChunkChoice represents a choice in a streaming chunk.
	chatChunkChoice struct {
		Index        int       `json:"index"`
		Delta        chatDelta `json:"delta"`
		FinishReason *string   `json:"finish_reason"`
	}

	// chatDelta represents the incremental content in a streaming chunk.
	chatDelta struct {
		Role      string                       `json:"role"`
		Content   json.RawMessage              `json:"content"`
		ToolCalls []chatStreamingToolCallDelta `json:"tool_calls"`
	}

	// chatStreamingToolCallDelta represents an incremental tool call in a streaming chunk.
	// Unlike chatToolCall, it carries an Index field used to correlate deltas across chunks.
	chatStreamingToolCallDelta struct {
		Index    int                  `json:"index"`
		ID       string               `json:"id"`
		Type     string               `json:"type"`
		Function chatToolCallFunction `json:"function"`
	}

	// chatChoice represents a single choice in a chat completion response.
	chatChoice struct {
		Index   int         `json:"index"`
		Message chatMessage `json:"message"`
	}

	// chatUsage represents token usage information in a chat completion response.
	chatUsage struct {
		PromptTokens            int                      `json:"prompt_tokens"`
		CompletionTokens        int                      `json:"completion_tokens"`
		TotalTokens             int                      `json:"total_tokens"`
		CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details"`
	}

	// completionTokensDetails represents the breakdown of completion token usage.
	completionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
		AudioTokens     int `json:"audio_tokens"`
	}
)

// sseDataPrefix is the prefic for the data lines of SSE events.
var sseDataPrefix = []byte("data: ")

// decodeChatRequest parses an OpenAI chat completions request body and extracts
// structured information for use in filter metadata.
func decodeChatRequest(body []byte) (*chatCompletionRequest, error) {
	var req chatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// decodeChatResponse parses an OpenAI chat completions response body and extracts
// structured information for use in filter metadata.
func decodeChatResponse(body []byte) (*chatCompletionResponse, error) {
	var resp chatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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

// sseAccumulator holds the incremental state for parsing a streaming SSE response.
// Data is fed via the feed method as body chunks arrive, and the final result is
// obtained by calling finish.
type sseAccumulator struct {
	buf               []byte
	done              bool
	usage             *chatUsage
	roleByChoice      map[int]string
	contentByChoice   map[int]string
	toolCallsByChoice map[int]map[int]*chatToolCall
	maxChoiceIdx      int
	logFn             func(string, ...any)
}

// newSSEAccumulator creates a new SSE accumulator.
func newSSEAccumulator(logFn func(string, ...any)) *sseAccumulator {
	return &sseAccumulator{
		roleByChoice:      map[int]string{},
		contentByChoice:   map[int]string{},
		toolCallsByChoice: map[int]map[int]*chatToolCall{},
		maxChoiceIdx:      -1,
		logFn:             logFn,
	}
}

// feed appends new data to the internal buffer and parses as many complete
// SSE events as possible. Parsed data is trimmed from the buffer.
func (a *sseAccumulator) feed(data []byte) {
	if a.done {
		return
	}
	a.buf = append(a.buf, data...)
	a.parseEvents()
}

// parseEvents consumes complete lines from the buffer and processes SSE events.
func (a *sseAccumulator) parseEvents() {
	for {
		// Find and read complete lines (terminated by \n).
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 { // If the buffer contains no more complete lines, stop.
			return
		}

		line := bytes.TrimSpace(a.buf[:idx]) // read the whole line
		a.buf = a.buf[idx+1:]                // remove the line bytes from the buffer
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue
		}
		data := bytes.TrimPrefix(line, sseDataPrefix)
		if bytes.Equal(data, []byte("[DONE]")) {
			a.done = true
			return
		}
		a.processChunk(data)
	}
}

// processChunk parses a single SSE data payload and accumulates it.
func (a *sseAccumulator) processChunk(data []byte) {
	var chunk chatCompletionChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		a.logFn("chat-completions-decoder: failed to parse streaming chunk: %s", err.Error())
		return
	}

	if chunk.Usage != nil {
		a.usage = chunk.Usage
	}

	for _, choice := range chunk.Choices {
		idx := choice.Index
		if idx > a.maxChoiceIdx {
			a.maxChoiceIdx = idx
		}

		if choice.Delta.Role != "" {
			a.roleByChoice[idx] = choice.Delta.Role
		}

		if content := extractContent(choice.Delta.Content); content != "" {
			a.contentByChoice[idx] += content
		}

		for _, tcDelta := range choice.Delta.ToolCalls {
			if _, ok := a.toolCallsByChoice[idx]; !ok {
				a.toolCallsByChoice[idx] = map[int]*chatToolCall{}
			}
			tcIdx := tcDelta.Index
			if tc, ok := a.toolCallsByChoice[idx][tcIdx]; ok {
				tc.Function.Arguments += tcDelta.Function.Arguments
			} else {
				a.toolCallsByChoice[idx][tcIdx] = &chatToolCall{
					ID:   tcDelta.ID,
					Type: tcDelta.Type,
					Function: chatToolCallFunction{
						Name:      tcDelta.Function.Name,
						Arguments: tcDelta.Function.Arguments,
					},
				}
			}
		}
	}
}

// finish assembles the accumulated state into a decodedResponse.
func (a *sseAccumulator) finish() *chatCompletionResponse {
	if a.maxChoiceIdx < 0 {
		return &chatCompletionResponse{Usage: a.usage}
	}

	choices := make([]chatChoice, a.maxChoiceIdx+1)
	for i := 0; i <= a.maxChoiceIdx; i++ {
		var rawContent json.RawMessage
		if content := a.contentByChoice[i]; content != "" {
			b, err := json.Marshal(content)
			if err != nil {
				a.logFn("chat-completions-decoder: failed to marshal content for choice %d: %s", i, err.Error())
			} else {
				rawContent = b
			}
		}

		var toolCalls []chatToolCall
		if tcMap, ok := a.toolCallsByChoice[i]; ok {
			tcIdxs := make([]int, 0, len(tcMap))
			for k := range tcMap {
				tcIdxs = append(tcIdxs, k)
			}
			sort.Ints(tcIdxs)
			for _, j := range tcIdxs {
				toolCalls = append(toolCalls, *tcMap[j])
			}
		}

		choices[i] = chatChoice{
			Index: i,
			Message: chatMessage{
				Role:      a.roleByChoice[i],
				Content:   rawContent,
				ToolCalls: toolCalls,
			},
		}
	}

	return &chatCompletionResponse{Choices: choices, Usage: a.usage}
}
