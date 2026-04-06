// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type anthropicRequest struct {
	Model    string                    `json:"model"`
	Stream   bool                      `json:"stream"`
	System   json.RawMessage           `json:"system"`
	Messages []anthropicRequestMessage `json:"messages"`
}

type anthropicRequestMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicUsage struct {
	InputTokens              uint32 `json:"input_tokens"`
	OutputTokens             uint32 `json:"output_tokens"`
	CacheCreationInputTokens uint32 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     uint32 `json:"cache_read_input_tokens"`
}

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

type anthropicToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type anthropicMessageStartData struct {
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

type anthropicMessageDeltaData struct {
	Usage struct {
		OutputTokens uint32 `json:"output_tokens"`
	} `json:"usage"`
}

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

type anthropicContentBlockDeltaData struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

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

type anthropicLLMResponseChunk struct {
	usage        LLMUsage
	hasTextToken bool
}

func (c *anthropicLLMResponseChunk) GetUsage() LLMUsage { return c.usage }
func (c *anthropicLLMResponseChunk) GetAnswer() string  { return "" }
func (c *anthropicLLMResponseChunk) GetReasoning() string {
	return ""
}
func (c *anthropicLLMResponseChunk) GetToolCalls() any          { return nil }
func (c *anthropicLLMResponseChunk) GetReasoningTokens() uint32 { return 0 }
func (c *anthropicLLMResponseChunk) GetCachedTokens() uint32    { return 0 }
func (c *anthropicLLMResponseChunk) GetInputTokenDetails() any  { return nil }
func (c *anthropicLLMResponseChunk) GetOutputTokenDetails() any { return nil }
func (c *anthropicLLMResponseChunk) HasTextToken() bool         { return c.hasTextToken }

type anthropicSSEParser struct {
	buf           []byte
	done          bool
	inputTokens   uint32
	outputTokens  uint32
	currentEvent  string
	textByIndex   map[int]string
	toolByIndex   map[int]*anthropicToolCall
	seenTextToken bool
}

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

func parseAnthropicChunk(eventType string, data []byte) (anthropicLLMResponseChunk, error) {
	switch eventType {
	case "message_start":
		var msg anthropicMessageStartData
		if err := json.Unmarshal(data, &msg); err != nil {
			return anthropicLLMResponseChunk{}, err
		}
		return anthropicLLMResponseChunk{usage: anthropicUsageToLLM(msg.Message.Usage)}, nil
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
	default:
		return anthropicLLMResponseChunk{}, nil
	}
}

func anthropicUsageToLLM(u anthropicUsage) LLMUsage {
	return LLMUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.InputTokens + u.OutputTokens,
	}
}

func newAnthropicSSEParser() *anthropicSSEParser {
	return &anthropicSSEParser{
		textByIndex: map[int]string{},
		toolByIndex: map[int]*anthropicToolCall{},
	}
}

func (a *anthropicSSEParser) Feed(data []byte) error {
	if a.done {
		return nil
	}
	a.buf = append(a.buf, data...)
	return a.parseEvents()
}

func (a *anthropicSSEParser) parseEvents() error {
	for {
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 {
			return nil
		}
		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+1:]

		if bytes.HasPrefix(line, []byte("event: ")) {
			a.currentEvent = string(bytes.TrimPrefix(line, []byte("event: ")))
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			payload := bytes.TrimPrefix(line, []byte("data: "))
			if err := a.processEvent(a.currentEvent, payload); err != nil {
				return err
			}
			a.currentEvent = ""
		}
	}
}

func (a *anthropicSSEParser) processEvent(eventType string, data []byte) error {
	if eventType == "message_stop" {
		a.done = true
		return nil
	}
	if eventType == "content_block_start" {
		var block anthropicContentBlockStartData
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("llm-statistics: failed to parse Anthropic SSE event %q: %w", eventType, err)
		}
		if block.ContentBlock.Type == "text" {
			a.textByIndex[block.Index] = block.ContentBlock.Text
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
			return fmt.Errorf("llm-statistics: failed to parse Anthropic SSE event %q: %w", eventType, err)
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
		return fmt.Errorf("llm-statistics: failed to parse Anthropic SSE event %q: %w", eventType, err)
	}
	if usage := chunk.GetUsage(); usage != (LLMUsage{}) {
		if usage.InputTokens > 0 {
			a.inputTokens = usage.InputTokens
		}
		if usage.OutputTokens > 0 {
			a.outputTokens = usage.OutputTokens
		}
	}
	return nil
}

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
	}, answer: answer, toolCalls: toolCalls}, nil
}

func (a *anthropicSSEParser) SeenTextToken() bool { return a.seenTextToken }

type anthropicFactory struct{}

func (f *anthropicFactory) ParseRequest(body []byte) (LLMRequest, error) {
	return parseAnthropicRequest(body)
}
func (f *anthropicFactory) ParseResponse(body []byte) (LLMResponse, error) {
	return parseAnthropicResponse(body)
}
func (f *anthropicFactory) NewSSEParser() SSEParser { return newAnthropicSSEParser() }

func extractAnthropicQuestion(messages []anthropicRequestMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		return extractAnthropicMessageContent(messages[i].Content)
	}
	return ""
}

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

func buildAnthropicInputTokenDetails(u anthropicUsage) any {
	if u.CacheCreationInputTokens == 0 && u.CacheReadInputTokens == 0 {
		return nil
	}
	return map[string]uint32{
		"cache_creation_input_tokens": u.CacheCreationInputTokens,
		"cache_read_input_tokens":     u.CacheReadInputTokens,
	}
}

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
