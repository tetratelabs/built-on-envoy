// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// openaiRequest is the minimal subset of an OpenAI Chat Completions request body
// needed to extract the model name and streaming flag.
type openaiRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// openaiUsage holds token-usage fields from an OpenAI response or chunk.
type openaiUsage struct {
	PromptTokens     uint32 `json:"prompt_tokens"`
	CompletionTokens uint32 `json:"completion_tokens"`
	TotalTokens      uint32 `json:"total_tokens"`
}

// openaiResponse is the minimal subset of an OpenAI Chat Completions response body.
type openaiResponse struct {
	Usage openaiUsage `json:"usage"`
}

// openaiChunk is a single data event in an OpenAI streaming SSE response.
type openaiChunk struct {
	Usage openaiUsage `json:"usage"`
}

// --- LLMRequest implementation ---

// openaiLLMRequest implements LLMRequest for the OpenAI Chat Completions API.
type openaiLLMRequest struct {
	model  string
	stream bool
}

func (r *openaiLLMRequest) GetModel() string { return r.model }
func (r *openaiLLMRequest) IsStream() bool   { return r.stream }

// parseOpenAIRequest parses an OpenAI Chat Completions request body and returns
// an LLMRequest with the extracted model and stream fields.
func parseOpenAIRequest(body []byte) (LLMRequest, error) {
	var req openaiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &openaiLLMRequest{model: req.Model, stream: req.Stream}, nil
}

// --- LLMResponse implementation ---

// openaiLLMResponse implements LLMResponse for the OpenAI Chat Completions API.
type openaiLLMResponse struct {
	usage LLMUsage
}

func (r *openaiLLMResponse) GetUsage() LLMUsage { return r.usage }

// parseOpenAIResponse parses an OpenAI Chat Completions response body and returns
// an LLMResponse with the extracted token-usage information.
func parseOpenAIResponse(body []byte) (LLMResponse, error) {
	var resp openaiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &openaiLLMResponse{usage: openaiUsageToLLM(resp.Usage)}, nil
}

// --- LLMResponseChunk implementation ---

// openaiLLMResponseChunk implements LLMResponseChunk for the OpenAI streaming API.
type openaiLLMResponseChunk struct {
	usage LLMUsage
}

func (c *openaiLLMResponseChunk) GetUsage() LLMUsage { return c.usage }

// parseOpenAIChunk parses a single data payload from an OpenAI streaming SSE response.
func parseOpenAIChunk(data []byte) (LLMResponseChunk, error) {
	var chunk openaiChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, err
	}
	return &openaiLLMResponseChunk{usage: openaiUsageToLLM(chunk.Usage)}, nil
}

// openaiUsageToLLM converts an openaiUsage to an LLMUsage.
// Returns the zero value when u is nil.
func openaiUsageToLLM(u openaiUsage) LLMUsage {
	return LLMUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
	}
}

// --- SSE accumulator ---

var openaiSSEDataPrefix = []byte("data: ")

// openaiSSEParser accumulates usage information from an OpenAI streaming SSE response.
// It consumes body chunks as they arrive and produces an LLMResponse when finished.
type openaiSSEParser struct {
	buf   []byte
	done  bool
	usage LLMUsage
}

func newOpenAISSEParser() *openaiSSEParser {
	return &openaiSSEParser{}
}

func (a *openaiSSEParser) Feed(data []byte) error {
	if a.done {
		return nil
	}
	a.buf = append(a.buf, data...)
	return a.parseEvents()
}

func (a *openaiSSEParser) parseEvents() error {
	for {
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 {
			return nil
		}
		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+1:]

		if !bytes.HasPrefix(line, openaiSSEDataPrefix) {
			continue
		}
		payload := bytes.TrimPrefix(line, openaiSSEDataPrefix)
		if bytes.Equal(payload, []byte("[DONE]")) {
			a.done = true
			return nil
		}
		chunk, err := parseOpenAIChunk(payload)
		if err != nil {
			return fmt.Errorf("llm-proxy: failed to parse OpenAI streaming chunk: %w", err)
		}
		if u := chunk.GetUsage(); u != (LLMUsage{}) {
			a.usage = u
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

func (a *openaiSSEParser) Finish() (LLMResponse, error) {
	return &openaiLLMResponse{usage: a.usage}, nil
}
