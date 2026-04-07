// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package llmproxy implements an HTTP filter that identifies LLM API requests
// and extracts model, stream, token-usage, and richer observability attributes
// into filter metadata.
package llmproxy

const (
	// KindOpenAI matches the OpenAI Chat Completions API.
	KindOpenAI string = "openai"
	// KindAnthropic matches the Anthropic Messages API.
	KindAnthropic string = "anthropic"
	// KindCustom matches a custom OpenAI-compatible API.
	// It uses the same request/response structure as OpenAI.
	KindCustom string = "custom"
)

// LLMUsage holds the token-usage counters extracted from an LLM API response.
type LLMUsage struct {
	// InputTokens is the number of tokens consumed by the prompt / input.
	InputTokens uint32
	// OutputTokens is the number of tokens produced by the completion / output.
	OutputTokens uint32
	// TotalTokens is the sum of InputTokens and OutputTokens.
	TotalTokens uint32
}

// LLMRequest abstracts over different LLM API request formats.
type LLMRequest interface {
	// GetModel returns the model name specified in the request.
	GetModel() string
	// IsStream returns whether the request asks for a streaming (SSE) response.
	IsStream() bool
	// GetQuestion returns the user question extracted from the request if available.
	GetQuestion() string
	// GetSystem returns the system prompt extracted from the request if available.
	GetSystem() string
}

// LLMResponse abstracts over different LLM API non-streaming response formats.
type LLMResponse interface {
	// GetUsage returns token-usage information extracted from the response body.
	// The zero value of LLMUsage indicates that no usage data was present.
	GetUsage() LLMUsage
	// GetAnswer returns the textual assistant answer extracted from the response if available.
	GetAnswer() string
	// GetReasoning returns provider-specific reasoning content when available.
	GetReasoning() string
	// GetToolCalls returns any tool call payload emitted by the model.
	GetToolCalls() any
	// GetReasoningTokens returns the number of reasoning tokens when provided.
	GetReasoningTokens() uint32
	// GetCachedTokens returns cached input token counts when provided.
	GetCachedTokens() uint32
	// GetInputTokenDetails returns provider-specific prompt/input token detail fields.
	GetInputTokenDetails() any
	// GetOutputTokenDetails returns provider-specific completion/output token detail fields.
	GetOutputTokenDetails() any
}

// LLMResponseChunk abstracts over a single event in an LLM streaming SSE response.
type LLMResponseChunk interface {
	// GetUsage returns token-usage information carried by this chunk.
	// The zero value of LLMUsage indicates that the chunk carries no usage data.
	GetUsage() LLMUsage
	// GetAnswer returns any assistant text carried by this chunk.
	GetAnswer() string
	// GetReasoning returns any reasoning content carried by this chunk.
	GetReasoning() string
	// GetToolCalls returns any tool call delta carried by this chunk.
	GetToolCalls() any
	// HasTextToken reports whether the chunk contains a real text token.
	HasTextToken() bool
}

// SSEParser incrementally consumes body chunks from an LLM streaming SSE response
// and produces an LLMResponse once the stream is complete.
type SSEParser interface {
	// Feed appends a new body chunk to the parser's internal buffer and processes
	// any complete SSE events it contains. It returns the first parse error
	// encountered in this chunk, if any; the caller is responsible for logging it.
	Feed(data []byte) error
	// Finish finalises parsing and returns the accumulated LLMResponse and any
	// terminal error encountered while processing the stream.
	Finish() (LLMResponse, error)
	// SeenTextToken reports whether the stream has emitted a real text token yet.
	SeenTextToken() bool
}

// LLMFactory creates the per-API-type parsers for a specific LLM provider.
type LLMFactory interface {
	// ParseRequest parses a complete request body and returns an LLMRequest.
	ParseRequest(body []byte) (LLMRequest, error)
	// ParseResponse parses a complete non-streaming response body and returns an LLMResponse.
	ParseResponse(body []byte) (LLMResponse, error)
	// NewSSEParser creates an SSEParser for accumulating a streaming SSE response.
	NewSSEParser() SSEParser
}
