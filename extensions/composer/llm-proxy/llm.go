// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package llmproxy implements an HTTP filter that identifies LLM API requests
// and extracts model, stream, and token-usage information into filter metadata.
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
}

// LLMResponse abstracts over different LLM API non-streaming response formats.
type LLMResponse interface {
	// GetUsage returns token-usage information extracted from the response body.
	// The zero value of LLMUsage indicates that no usage data was present.
	GetUsage() LLMUsage
}

// LLMResponseChunk abstracts over a single event in an LLM streaming SSE response.
type LLMResponseChunk interface {
	// GetUsage returns token-usage information carried by this chunk.
	// The zero value of LLMUsage indicates that the chunk carries no usage data.
	GetUsage() LLMUsage
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
