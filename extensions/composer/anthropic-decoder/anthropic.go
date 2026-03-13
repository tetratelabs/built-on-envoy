// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"bytes"
	"encoding/json"
	"strings"
)

type (
	// anthropicRequest represents an Anthropic Messages API request.
	anthropicRequest struct {
		Model    string             `json:"model"`
		System   json.RawMessage    `json:"system"`
		Messages []anthropicMessage `json:"messages"`
		Tools    []anthropicTool    `json:"tools"`
	}

	// anthropicMessage represents a single message in the Anthropic format.
	anthropicMessage struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	// anthropicContentBlock represents a content block in the Anthropic format.
	anthropicContentBlock struct {
		Type      string          `json:"type"`                  // "text", "tool_use", "tool_result"
		Text      string          `json:"text,omitempty"`        // text block
		ID        string          `json:"id,omitempty"`          // tool_use
		Name      string          `json:"name,omitempty"`        // tool_use
		Input     json.RawMessage `json:"input,omitempty"`       // tool_use
		ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
		Content   json.RawMessage `json:"content,omitempty"`     // tool_result (string or blocks)
	}

	// anthropicTool represents a tool definition in the Anthropic format.
	anthropicTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}

	// anthropicResponse represents an Anthropic Messages API response.
	anthropicResponse struct {
		ID      string                   `json:"id"`
		Type    string                   `json:"type"` // "message"
		Role    string                   `json:"role"` // "assistant"
		Model   string                   `json:"model"`
		Content []*anthropicContentBlock `json:"content"`
		Usage   *anthropicUsage          `json:"usage"`
	}

	// anthropicUsage represents token usage information in an Anthropic response.
	anthropicUsage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	}

	// anthropicMessageStart represents the message_start SSE event payload.
	anthropicMessageStart struct {
		Type    string            `json:"type"`
		Message anthropicResponse `json:"message"`
	}

	// anthropicContentBlockStart represents the content_block_start SSE event payload.
	anthropicContentBlockStart struct {
		Type         string                `json:"type"`
		Index        int                   `json:"index"`
		ContentBlock anthropicContentBlock `json:"content_block"`
	}

	// anthropicContentBlockDelta represents the content_block_delta SSE event payload.
	anthropicContentBlockDelta struct {
		Type  string         `json:"type"`
		Index int            `json:"index"`
		Delta anthropicDelta `json:"delta"`
	}

	// anthropicDelta represents the delta content in a streaming chunk.
	anthropicDelta struct {
		Type        string `json:"type"`         // "text_delta" or "input_json_delta"
		Text        string `json:"text"`         // for text_delta
		PartialJSON string `json:"partial_json"` // for input_json_delta
	}

	// anthropicMessageDelta represents the message_delta SSE event payload.
	anthropicMessageDelta struct {
		Type  string                    `json:"type"`
		Delta anthropicMessageDeltaBody `json:"delta"`
		Usage *anthropicDeltaUsage      `json:"usage"`
	}

	// anthropicMessageDeltaBody represents the delta body in a message_delta event.
	anthropicMessageDeltaBody struct {
		StopReason string `json:"stop_reason"`
	}

	// anthropicDeltaUsage represents the usage in a message_delta event.
	anthropicDeltaUsage struct {
		OutputTokens int `json:"output_tokens"`
	}
)

// SSE line prefixes.
var (
	sseEventPrefix = []byte("event: ")
	sseDataPrefix  = []byte("data: ")
)

// decodeAnthropicRequest parses an Anthropic Messages API request body.
func decodeAnthropicRequest(body []byte) (*anthropicRequest, error) {
	var req anthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// decodeAnthropicResponse parses an Anthropic Messages API response body.
func decodeAnthropicResponse(body []byte) (*anthropicResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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

// extractAnthropicToolCalls extracts tool_use content blocks from a message's
// content field. Returns nil if content is a string or contains no tool_use blocks.
func extractAnthropicToolCalls(raw json.RawMessage) []*anthropicContentBlock {
	if len(raw) == 0 {
		return nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var toolCalls []*anthropicContentBlock
	for i := range blocks {
		if blocks[i].Type == "tool_use" {
			toolCalls = append(toolCalls, &blocks[i])
		}
	}
	return toolCalls
}

// anthropicSSEAccumulator holds the incremental state for parsing an Anthropic
// streaming SSE response. Data is fed via the feed method as body chunks arrive,
// and the final result is obtained by calling finish.
type anthropicSSEAccumulator struct {
	buf  []byte
	done bool

	// Accumulated state from SSE events.
	role          string
	usage         *anthropicUsage
	contentBlocks []*anthropicContentBlock // indexed by content_block_start index
	textByIndex   map[int]string           // accumulated text per content block index
	jsonByIndex   map[int]string           // accumulated partial JSON per content block index (tool_use)

	// SSE parse state.
	currentEvent string // the most recently seen "event:" value

	logFn func(string, ...any)
}

// newAnthropicSSEAccumulator creates a new Anthropic SSE accumulator.
func newAnthropicSSEAccumulator(logFn func(string, ...any)) *anthropicSSEAccumulator {
	return &anthropicSSEAccumulator{
		textByIndex: map[int]string{},
		jsonByIndex: map[int]string{},
		logFn:       logFn,
	}
}

// feed appends new data to the internal buffer and parses as many complete
// SSE events as possible.
func (a *anthropicSSEAccumulator) feed(data []byte) {
	if a.done {
		return
	}
	a.buf = append(a.buf, data...)
	a.parseEvents()
}

// parseEvents consumes complete lines from the buffer and processes SSE events.
func (a *anthropicSSEAccumulator) parseEvents() {
	for {
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 {
			return
		}

		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+1:]

		if bytes.HasPrefix(line, sseEventPrefix) {
			a.currentEvent = string(bytes.TrimPrefix(line, sseEventPrefix))
			continue
		}

		if bytes.HasPrefix(line, sseDataPrefix) {
			data := bytes.TrimPrefix(line, sseDataPrefix)
			a.processEvent(a.currentEvent, data)
			a.currentEvent = "" // reset after processing
		}
	}
}

// processEvent handles a single SSE event based on its type.
func (a *anthropicSSEAccumulator) processEvent(eventType string, data []byte) {
	switch eventType {
	case "message_start":
		var msg anthropicMessageStart
		if err := json.Unmarshal(data, &msg); err != nil {
			a.logFn("anthropic-messages-decoder: failed to parse message_start: %s", err.Error())
			return
		}
		a.role = msg.Message.Role
		if msg.Message.Usage != nil {
			a.usage = msg.Message.Usage
		}

	case "content_block_start":
		var cbs anthropicContentBlockStart
		if err := json.Unmarshal(data, &cbs); err != nil {
			a.logFn("anthropic-messages-decoder: failed to parse content_block_start: %s", err.Error())
			return
		}
		// Grow the slice to accommodate the index.
		for len(a.contentBlocks) <= cbs.Index {
			a.contentBlocks = append(a.contentBlocks, &anthropicContentBlock{})
		}
		a.contentBlocks[cbs.Index] = &cbs.ContentBlock

	case "content_block_delta":
		var cbd anthropicContentBlockDelta
		if err := json.Unmarshal(data, &cbd); err != nil {
			a.logFn("anthropic-messages-decoder: failed to parse content_block_delta: %s", err.Error())
			return
		}
		switch cbd.Delta.Type {
		case "text_delta":
			a.textByIndex[cbd.Index] += cbd.Delta.Text
		case "input_json_delta":
			a.jsonByIndex[cbd.Index] += cbd.Delta.PartialJSON
		}

	case "message_delta":
		var md anthropicMessageDelta
		if err := json.Unmarshal(data, &md); err != nil {
			a.logFn("anthropic-messages-decoder: failed to parse message_delta: %s", err.Error())
			return
		}
		if md.Usage != nil && a.usage != nil {
			a.usage.OutputTokens = md.Usage.OutputTokens
		}

	case "message_stop":
		a.done = true
	}
}

// finish assembles the accumulated state into an anthropicResponse.
func (a *anthropicSSEAccumulator) finish() *anthropicResponse {
	content := make([]*anthropicContentBlock, len(a.contentBlocks))
	for i, block := range a.contentBlocks {
		content[i] = block
		switch block.Type {
		case "text":
			if text, ok := a.textByIndex[i]; ok {
				content[i].Text = text
			}
		case "tool_use":
			if jsonStr, ok := a.jsonByIndex[i]; ok {
				content[i].Input = json.RawMessage(jsonStr)
			}
		}
	}

	return &anthropicResponse{
		Role:    a.role,
		Content: content,
		Usage:   a.usage,
	}
}
