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

// openAIRequest is the subset of an OpenAI Chat Completions request body
// needed for routing and richer observability extraction.
type openAIRequest struct {
	Model    string                 `json:"model"`
	Stream   bool                   `json:"stream"`
	Messages []openAIRequestMessage `json:"messages"`
}

// openAIRequestMessage models one request message in a Chat Completions payload.
type openAIRequestMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// openAIContentPart is used to extract text from array-form message content.
type openAIContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// openAIUsage holds token-usage fields from an OpenAI response or chunk.
type openAIUsage struct {
	PromptTokens            uint32                         `json:"prompt_tokens"`
	CompletionTokens        uint32                         `json:"completion_tokens"`
	TotalTokens             uint32                         `json:"total_tokens"`
	PromptTokensDetails     *openAIPromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *openAICompletionTokensDetails `json:"completion_tokens_details"`
}

// openAIPromptTokensDetails holds provider-specific prompt token detail fields.
type openAIPromptTokensDetails struct {
	CachedTokens uint32 `json:"cached_tokens"`
}

// openAICompletionTokensDetails holds provider-specific completion token detail fields.
type openAICompletionTokensDetails struct {
	ReasoningTokens uint32 `json:"reasoning_tokens"`
	AudioTokens     uint32 `json:"audio_tokens"`
}

// openAIResponse is the subset of a non-streaming Chat Completions response
// needed for richer observability extraction.
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content          string           `json:"content"`
			ReasoningContent string           `json:"reasoning_content"`
			ToolCalls        []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage openAIUsage `json:"usage"`
}

// openAIChunk is a single data event in an OpenAI streaming SSE response.
type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content          string                         `json:"content"`
			ReasoningContent string                         `json:"reasoning_content"`
			ToolCalls        []openAIStreamingToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage openAIUsage `json:"usage"`
}

// openAIToolCall represents a tool call in a non-streaming response.
type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

// openAIToolCallFunction represents the function call payload of a tool call.
type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openAIStreamingToolCallDelta represents an incremental tool call update in a stream.
type openAIStreamingToolCallDelta struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

// openAILLMRequest implements LLMRequest for the OpenAI Chat Completions API.
type openAILLMRequest struct {
	model    string
	stream   bool
	question string
	system   string
}

func (r *openAILLMRequest) GetModel() string { return r.model }
func (r *openAILLMRequest) IsStream() bool   { return r.stream }
func (r *openAILLMRequest) GetQuestion() string {
	return r.question
}
func (r *openAILLMRequest) GetSystem() string { return r.system }

// openAILLMResponse implements LLMResponse for the OpenAI Chat Completions API.
type openAILLMResponse struct {
	usage              LLMUsage
	answer             string
	reasoning          string
	toolCalls          []openAIToolCall
	reasoningTokens    uint32
	cachedTokens       uint32
	inputTokenDetails  any
	outputTokenDetails any
}

func (r *openAILLMResponse) GetUsage() LLMUsage { return r.usage }
func (r *openAILLMResponse) GetAnswer() string  { return r.answer }
func (r *openAILLMResponse) GetReasoning() string {
	return r.reasoning
}
func (r *openAILLMResponse) GetToolCalls() any          { return r.toolCalls }
func (r *openAILLMResponse) GetReasoningTokens() uint32 { return r.reasoningTokens }
func (r *openAILLMResponse) GetCachedTokens() uint32    { return r.cachedTokens }
func (r *openAILLMResponse) GetInputTokenDetails() any  { return r.inputTokenDetails }
func (r *openAILLMResponse) GetOutputTokenDetails() any { return r.outputTokenDetails }

// openAILLMResponseChunk implements LLMResponseChunk for the OpenAI streaming API.
type openAILLMResponseChunk struct {
	usage              LLMUsage
	answer             string
	reasoning          string
	toolCalls          []openAIStreamingToolCallDelta
	cachedTokens       uint32
	reasoningTokens    uint32
	inputTokenDetails  any
	outputTokenDetails any
}

func (c *openAILLMResponseChunk) GetUsage() LLMUsage { return c.usage }
func (c *openAILLMResponseChunk) GetAnswer() string  { return c.answer }
func (c *openAILLMResponseChunk) GetReasoning() string {
	return c.reasoning
}
func (c *openAILLMResponseChunk) GetToolCalls() any { return c.toolCalls }
func (c *openAILLMResponseChunk) HasTextToken() bool {
	return c.answer != "" || c.reasoning != ""
}

// openAISSEParser accumulates usage, text, reasoning, tool calls, and token-detail
// fields from an OpenAI streaming SSE response and produces an LLMResponse when finished.
type openAISSEParser struct {
	buf                []byte
	done               bool
	usage              LLMUsage
	answer             string
	reasoning          string
	toolCallsByIndex   map[int]*openAIToolCall
	seenTextToken      bool
	cachedTokens       uint32
	reasoningTokens    uint32
	inputTokenDetails  any
	outputTokenDetails any
}

// parseOpenAIRequest parses an OpenAI Chat Completions request body and returns
// an LLMRequest with routing and observability fields.
func parseOpenAIRequest(body []byte) (LLMRequest, error) {
	var req openAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &openAILLMRequest{
		model:    req.Model,
		stream:   req.Stream,
		question: extractOpenAIQuestion(req.Messages),
		system:   extractOpenAISystem(req.Messages),
	}, nil
}

// parseOpenAIResponse parses a non-streaming OpenAI Chat Completions response
// and extracts token usage plus richer observability fields.
func parseOpenAIResponse(body []byte) (LLMResponse, error) {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	result := &openAILLMResponse{usage: openAIUsageToLLM(resp.Usage)}
	if len(resp.Choices) > 0 {
		result.answer = resp.Choices[0].Message.Content
		result.reasoning = resp.Choices[0].Message.ReasoningContent
		result.toolCalls = resp.Choices[0].Message.ToolCalls
	}
	if resp.Usage.PromptTokensDetails != nil {
		result.cachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
		result.inputTokenDetails = resp.Usage.PromptTokensDetails
	}
	if resp.Usage.CompletionTokensDetails != nil {
		result.reasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
		result.outputTokenDetails = resp.Usage.CompletionTokensDetails
	}
	return result, nil
}

// parseOpenAIChunk parses a single OpenAI streaming SSE payload.
func parseOpenAIChunk(data []byte) (LLMResponseChunk, error) {
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, err
	}
	result := &openAILLMResponseChunk{usage: openAIUsageToLLM(chunk.Usage)}
	if len(chunk.Choices) > 0 {
		result.answer = chunk.Choices[0].Delta.Content
		result.reasoning = chunk.Choices[0].Delta.ReasoningContent
		result.toolCalls = chunk.Choices[0].Delta.ToolCalls
	}
	if chunk.Usage.PromptTokensDetails != nil {
		result.cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		result.inputTokenDetails = chunk.Usage.PromptTokensDetails
	}
	if chunk.Usage.CompletionTokensDetails != nil {
		result.reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		result.outputTokenDetails = chunk.Usage.CompletionTokensDetails
	}
	return result, nil
}

// openAIUsageToLLM converts an OpenAI usage payload to the common LLMUsage shape.
func openAIUsageToLLM(u openAIUsage) LLMUsage {
	return LLMUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
	}
}

// newOpenAISSEParser creates a parser for incremental OpenAI SSE accumulation.
func newOpenAISSEParser() *openAISSEParser {
	return &openAISSEParser{
		toolCallsByIndex: map[int]*openAIToolCall{},
	}
}

// Feed appends a new response body chunk and parses any complete SSE events.
func (a *openAISSEParser) Feed(data []byte) error {
	if a.done {
		return nil
	}
	a.buf = append(a.buf, data...)
	return a.parseEvents()
}

// parseEvents processes complete SSE lines accumulated in the internal buffer.
func (a *openAISSEParser) parseEvents() error {
	for {
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 {
			return nil
		}
		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+1:]

		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(payload, []byte("[DONE]")) {
			a.done = true
			return nil
		}
		chunk, err := parseOpenAIChunk(payload)
		if err != nil {
			return fmt.Errorf("llm-proxy: failed to parse OpenAI streaming chunk: %w", err)
		}
		if usage := chunk.GetUsage(); usage != (LLMUsage{}) {
			a.usage = usage
		}
		if chunkOpenAI, ok := chunk.(*openAILLMResponseChunk); ok {
			if chunkOpenAI.cachedTokens > 0 {
				a.cachedTokens = chunkOpenAI.cachedTokens
			}
			if chunkOpenAI.reasoningTokens > 0 {
				a.reasoningTokens = chunkOpenAI.reasoningTokens
			}
			if chunkOpenAI.inputTokenDetails != nil {
				a.inputTokenDetails = chunkOpenAI.inputTokenDetails
			}
			if chunkOpenAI.outputTokenDetails != nil {
				a.outputTokenDetails = chunkOpenAI.outputTokenDetails
			}
		}
		if chunk.HasTextToken() {
			a.seenTextToken = true
		}
		a.answer += chunk.GetAnswer()
		a.reasoning += chunk.GetReasoning()
		for _, delta := range chunk.GetToolCalls().([]openAIStreamingToolCallDelta) {
			if tc, ok := a.toolCallsByIndex[delta.Index]; ok {
				if delta.ID != "" {
					tc.ID = delta.ID
				}
				if delta.Type != "" {
					tc.Type = delta.Type
				}
				if delta.Function.Name != "" {
					tc.Function.Name = delta.Function.Name
				}
				tc.Function.Arguments += delta.Function.Arguments
			} else {
				a.toolCallsByIndex[delta.Index] = &openAIToolCall{
					ID:   delta.ID,
					Type: delta.Type,
					Function: openAIToolCallFunction{
						Name:      delta.Function.Name,
						Arguments: delta.Function.Arguments,
					},
				}
			}
		}
	}
}

// openaiFactory implements LLMFactory for the OpenAI Chat Completions API.
type openaiFactory struct{}

func (f *openaiFactory) ParseRequest(body []byte) (LLMRequest, error) {
	return parseOpenAIRequest(body)
}

func (f *openaiFactory) ParseResponse(body []byte) (LLMResponse, error) {
	return parseOpenAIResponse(body)
}

func (f *openaiFactory) NewSSEParser() SSEParser { return newOpenAISSEParser() }

// Finish finalises the stream and returns the accumulated response fields.
func (a *openAISSEParser) Finish() (LLMResponse, error) {
	var toolCalls []openAIToolCall
	if len(a.toolCallsByIndex) > 0 {
		indexes := make([]int, 0, len(a.toolCallsByIndex))
		for idx := range a.toolCallsByIndex {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			toolCalls = append(toolCalls, *a.toolCallsByIndex[idx])
		}
	}
	return &openAILLMResponse{
		usage:              a.usage,
		answer:             a.answer,
		reasoning:          a.reasoning,
		toolCalls:          toolCalls,
		cachedTokens:       a.cachedTokens,
		reasoningTokens:    a.reasoningTokens,
		inputTokenDetails:  a.inputTokenDetails,
		outputTokenDetails: a.outputTokenDetails,
	}, nil
}

// SeenTextToken reports whether the stream has emitted a real text token yet.
func (a *openAISSEParser) SeenTextToken() bool { return a.seenTextToken }

// extractOpenAIQuestion returns the last user message content from the request.
func extractOpenAIQuestion(messages []openAIRequestMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		return extractOpenAIMessageContent(messages[i].Content)
	}
	return ""
}

// extractOpenAISystem returns the first system message content from the request.
func extractOpenAISystem(messages []openAIRequestMessage) string {
	for i := 0; i < len(messages); i++ {
		if messages[i].Role != "system" {
			continue
		}
		return extractOpenAIMessageContent(messages[i].Content)
	}
	return ""
}

// extractOpenAIMessageContent extracts text from either string or array-form content.
func extractOpenAIMessageContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	var parts []openAIContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type == "text" && p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}
