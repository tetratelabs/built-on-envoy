// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// anthropicRequest is the subset of an Anthropic Messages request body
// needed for routing and richer observability extraction.
type anthropicRequest struct {
	Model    string                    `json:"model"`
	Stream   bool                      `json:"stream"`
	System   json.RawMessage           `json:"system"`
	Messages []anthropicRequestMessage `json:"messages"`
}

// anthropicRequestMessage models one request message in the Anthropic payload.
type anthropicRequestMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// anthropicUsage holds token-usage and cache-related fields from an Anthropic response.
type anthropicUsage struct {
	InputTokens              uint32 `json:"input_tokens"`
	OutputTokens             uint32 `json:"output_tokens"`
	CacheCreationInputTokens uint32 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     uint32 `json:"cache_read_input_tokens"`
}

// anthropicResponse is the subset of a non-streaming Anthropic response
// needed for richer observability extraction.
type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	Usage anthropicUsage `json:"usage"`
}

// anthropicToolCall represents a tool use block in Anthropic responses.
type anthropicToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

// anthropicMessageStartData is the payload of a "message_start" SSE event.
type anthropicMessageStartData struct {
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

// anthropicMessageDeltaData is the payload of a "message_delta" SSE event.
type anthropicMessageDeltaData struct {
	Usage struct {
		OutputTokens uint32 `json:"output_tokens"`
	} `json:"usage"`
}

// anthropicContentBlockStartData is the payload of a "content_block_start" SSE event.
type anthropicContentBlockStartData struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content_block"`
}

// anthropicContentBlockDeltaData is the payload of a "content_block_delta" SSE event.
type anthropicContentBlockDeltaData struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

// anthropicLLMRequest implements LLMRequest for the Anthropic Messages API.
type anthropicLLMRequest struct {
	model    string
	stream   bool
	question string
	system   string
}

func (r *anthropicLLMRequest) GetModel() string { return r.model }
func (r *anthropicLLMRequest) IsStream() bool   { return r.stream }
func (r *anthropicLLMRequest) GetQuestion() string {
	return r.question
}
func (r *anthropicLLMRequest) GetSystem() string { return r.system }

// anthropicLLMResponse implements LLMResponse for the Anthropic Messages API.
type anthropicLLMResponse struct {
	usage              LLMUsage
	answer             string
	reasoning          string
	toolCalls          []anthropicToolCall
	reasoningTokens    uint32
	cachedTokens       uint32
	inputTokenDetails  any
	outputTokenDetails any
}

func (r *anthropicLLMResponse) GetUsage() LLMUsage { return r.usage }
func (r *anthropicLLMResponse) GetAnswer() string  { return r.answer }
func (r *anthropicLLMResponse) GetReasoning() string {
	return r.reasoning
}
func (r *anthropicLLMResponse) GetToolCalls() any          { return r.toolCalls }
func (r *anthropicLLMResponse) GetReasoningTokens() uint32 { return r.reasoningTokens }
func (r *anthropicLLMResponse) GetCachedTokens() uint32    { return r.cachedTokens }
func (r *anthropicLLMResponse) GetInputTokenDetails() any  { return r.inputTokenDetails }
func (r *anthropicLLMResponse) GetOutputTokenDetails() any { return r.outputTokenDetails }

// anthropicLLMResponseChunk implements LLMResponseChunk for Anthropic streaming SSE.
type anthropicLLMResponseChunk struct {
	usage             LLMUsage
	hasTextToken      bool
	cachedTokens      uint32
	inputTokenDetails any
}

func (c *anthropicLLMResponseChunk) GetUsage() LLMUsage { return c.usage }
func (c *anthropicLLMResponseChunk) GetAnswer() string  { return "" }
func (c *anthropicLLMResponseChunk) GetReasoning() string {
	return ""
}
func (c *anthropicLLMResponseChunk) GetToolCalls() any { return nil }
func (c *anthropicLLMResponseChunk) HasTextToken() bool {
	return c.hasTextToken
}

// parseAnthropicRequest parses an Anthropic Messages request body and returns
// an LLMRequest with routing and observability fields.
func parseAnthropicRequest(body []byte) (LLMRequest, error) {
	var req anthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &anthropicLLMRequest{
		model:    req.Model,
		stream:   req.Stream,
		question: extractAnthropicQuestion(req.Messages),
		system:   extractAnthropicSystem(req.System),
	}, nil
}

// parseAnthropicResponse parses a non-streaming Anthropic response and extracts
// usage plus richer observability fields.
func parseAnthropicResponse(body []byte) (LLMResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	answer := ""
	toolCalls := make([]anthropicToolCall, 0)
	for _, item := range resp.Content {
		if item.Type == "text" {
			answer += item.Text
		}
		if item.Type == "tool_use" {
			toolCalls = append(toolCalls, anthropicToolCall{
				ID:    item.ID,
				Name:  item.Name,
				Input: string(item.Input),
			})
		}
	}
	return &anthropicLLMResponse{
		usage:             anthropicUsageToLLM(resp.Usage),
		answer:            answer,
		toolCalls:         toolCalls,
		cachedTokens:      resp.Usage.CacheReadInputTokens,
		inputTokenDetails: buildAnthropicInputTokenDetails(resp.Usage),
	}, nil
}

// parseAnthropicChunk parses a single Anthropic SSE event payload.
func parseAnthropicChunk(eventType string, data []byte) (anthropicLLMResponseChunk, error) {
	switch eventType {
	case "message_start":
		var msg anthropicMessageStartData
		if err := json.Unmarshal(data, &msg); err != nil {
			return anthropicLLMResponseChunk{}, err
		}
		return anthropicLLMResponseChunk{
			usage:             anthropicUsageToLLM(msg.Message.Usage),
			cachedTokens:      msg.Message.Usage.CacheReadInputTokens,
			inputTokenDetails: buildAnthropicInputTokenDetails(msg.Message.Usage),
		}, nil

	case "message_delta":
		var delta anthropicMessageDeltaData
		if err := json.Unmarshal(data, &delta); err != nil {
			return anthropicLLMResponseChunk{}, err
		}
		return anthropicLLMResponseChunk{usage: LLMUsage{OutputTokens: delta.Usage.OutputTokens}}, nil
	case "content_block_delta":
		var delta anthropicContentBlockDeltaData
		if err := json.Unmarshal(data, &delta); err != nil {
			return anthropicLLMResponseChunk{}, err
		}
		return anthropicLLMResponseChunk{hasTextToken: delta.Delta.Type == "text_delta" && delta.Delta.Text != ""}, nil
	}
	return anthropicLLMResponseChunk{}, nil
}

// anthropicUsageToLLM converts an Anthropic usage payload to the common LLMUsage shape.
func anthropicUsageToLLM(u anthropicUsage) LLMUsage {
	return LLMUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.InputTokens + u.OutputTokens,
	}
}

var (
	anthropicSSEEventPrefix = []byte("event: ")
	anthropicSSEDataPrefix  = []byte("data: ")
)

// anthropicSSEParser accumulates usage, text, tool calls, and cache-related
// fields from an Anthropic streaming SSE response.
type anthropicSSEParser struct {
	buf               []byte
	done              bool
	inputTokens       uint32
	outputTokens      uint32
	cachedTokens      uint32
	currentEvent      string
	textByIndex       map[int]string
	toolByIndex       map[int]*anthropicToolCall
	seenTextToken     bool
	inputTokenDetails any
}

// newAnthropicSSEParser creates a parser for incremental Anthropic SSE accumulation.
func newAnthropicSSEParser() *anthropicSSEParser {
	return &anthropicSSEParser{
		textByIndex: map[int]string{},
		toolByIndex: map[int]*anthropicToolCall{},
	}
}

// Feed appends a new response body chunk and parses any complete SSE events.
func (a *anthropicSSEParser) Feed(data []byte) error {
	if a.done {
		return nil
	}
	a.buf = append(a.buf, data...)
	return a.parseEvents()
}

// parseEvents processes complete SSE lines accumulated in the internal buffer.
func (a *anthropicSSEParser) parseEvents() error {
	for {
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 {
			return nil
		}
		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+1:]

		if bytes.HasPrefix(line, anthropicSSEEventPrefix) {
			a.currentEvent = string(bytes.TrimPrefix(line, anthropicSSEEventPrefix))
			continue
		}
		if bytes.HasPrefix(line, anthropicSSEDataPrefix) {
			payload := bytes.TrimPrefix(line, anthropicSSEDataPrefix)
			if err := a.processEvent(a.currentEvent, payload); err != nil {
				return err
			}
			a.currentEvent = ""
		}
	}
}

// processEvent handles a single parsed Anthropic SSE event.
func (a *anthropicSSEParser) processEvent(eventType string, data []byte) error {
	if eventType == "message_stop" {
		a.done = true
		return nil
	}
	if eventType == "content_block_start" {
		var block anthropicContentBlockStartData
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("llm-proxy: failed to parse Anthropic SSE event %q: %w", eventType, err)
		}
		if block.ContentBlock.Type == "text" {
			a.textByIndex[block.Index] = block.ContentBlock.Text
			if block.ContentBlock.Text != "" {
				a.seenTextToken = true
			}
			return nil
		}
		if block.ContentBlock.Type == "tool_use" {
			a.toolByIndex[block.Index] = &anthropicToolCall{
				ID:    block.ContentBlock.ID,
				Name:  block.ContentBlock.Name,
				Input: string(block.ContentBlock.Input),
			}
		}
		return nil
	}
	if eventType == "content_block_delta" {
		var delta anthropicContentBlockDeltaData
		if err := json.Unmarshal(data, &delta); err != nil {
			return fmt.Errorf("llm-proxy: failed to parse Anthropic SSE event %q: %w", eventType, err)
		}
		switch delta.Delta.Type {
		case "text_delta":
			a.seenTextToken = true
			a.textByIndex[delta.Index] += delta.Delta.Text
		case "input_json_delta":
			if tc, ok := a.toolByIndex[delta.Index]; ok {
				if tc.Input == "" || tc.Input == "{}" {
					tc.Input = delta.Delta.PartialJSON
				} else {
					tc.Input += delta.Delta.PartialJSON
				}
			}
		}
		return nil
	}
	chunk, err := parseAnthropicChunk(eventType, data)
	if err != nil {
		return fmt.Errorf("llm-proxy: failed to parse Anthropic SSE event %q: %w", eventType, err)
	}
	if u := chunk.GetUsage(); u != (LLMUsage{}) {
		if u.InputTokens > 0 {
			a.inputTokens = u.InputTokens
		}
		if u.OutputTokens > 0 {
			a.outputTokens = u.OutputTokens
		}
	}
	if chunk.cachedTokens > 0 {
		a.cachedTokens = chunk.cachedTokens
	}
	if chunk.inputTokenDetails != nil {
		a.inputTokenDetails = chunk.inputTokenDetails
	}
	return nil
}

// Finish finalises the stream and returns the accumulated response fields.
func (a *anthropicSSEParser) Finish() (LLMResponse, error) {
	answer := ""
	if len(a.textByIndex) > 0 {
		indexes := make([]int, 0, len(a.textByIndex))
		for idx := range a.textByIndex {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			answer += a.textByIndex[idx]
		}
	}
	toolCalls := make([]anthropicToolCall, 0, len(a.toolByIndex))
	if len(a.toolByIndex) > 0 {
		indexes := make([]int, 0, len(a.toolByIndex))
		for idx := range a.toolByIndex {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			toolCalls = append(toolCalls, *a.toolByIndex[idx])
		}
	}
	return &anthropicLLMResponse{usage: LLMUsage{
		InputTokens:  a.inputTokens,
		OutputTokens: a.outputTokens,
		TotalTokens:  a.inputTokens + a.outputTokens,
	}, answer: answer, toolCalls: toolCalls, cachedTokens: a.cachedTokens, inputTokenDetails: a.inputTokenDetails}, nil
}

type anthropicFactory struct{}

func (f *anthropicFactory) ParseRequest(body []byte) (LLMRequest, error) {
	return parseAnthropicRequest(body)
}

func (f *anthropicFactory) ParseResponse(body []byte) (LLMResponse, error) {
	return parseAnthropicResponse(body)
}

func (f *anthropicFactory) NewSSEParser() SSEParser { return newAnthropicSSEParser() }

// SeenTextToken reports whether the stream has emitted a real text token yet.
func (a *anthropicSSEParser) SeenTextToken() bool { return a.seenTextToken }

// extractAnthropicQuestion returns the last user message content from the request.
func extractAnthropicQuestion(messages []anthropicRequestMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		return extractAnthropicMessageContent(messages[i].Content)
	}
	return ""
}

// extractAnthropicSystem returns the system prompt from either string or block form.
func extractAnthropicSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		out := ""
		for i, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if i > 0 && out != "" {
					out += "\n"
				}
				out += b.Text
			}
		}
		return out
	}
	return ""
}

// buildAnthropicInputTokenDetails returns cache-related input token detail fields when present.
func buildAnthropicInputTokenDetails(u anthropicUsage) any {
	if u.CacheCreationInputTokens == 0 && u.CacheReadInputTokens == 0 {
		return nil
	}
	return map[string]uint32{
		"cache_creation_input_tokens": u.CacheCreationInputTokens,
		"cache_read_input_tokens":     u.CacheReadInputTokens,
	}
}

// extractAnthropicMessageContent extracts text from either string or block-form content.
func extractAnthropicMessageContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	texts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}
