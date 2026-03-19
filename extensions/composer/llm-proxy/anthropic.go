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

// anthropicRequest is the minimal subset of an Anthropic Messages API request body
// needed to extract the model name and streaming flag.
type anthropicRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// anthropicUsage holds token-usage fields from an Anthropic response.
type anthropicUsage struct {
	InputTokens  uint32 `json:"input_tokens"`
	OutputTokens uint32 `json:"output_tokens"`
}

// anthropicResponse is the minimal subset of an Anthropic Messages API response body.
type anthropicResponse struct {
	Usage anthropicUsage `json:"usage"`
}

// anthropicMessageStartData is the payload of an Anthropic "message_start" SSE event.
type anthropicMessageStartData struct {
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

// anthropicMessageDeltaData is the payload of an Anthropic "message_delta" SSE event.
type anthropicMessageDeltaData struct {
	Usage struct {
		OutputTokens uint32 `json:"output_tokens"`
	} `json:"usage"`
}

// --- LLMRequest implementation ---

// anthropicLLMRequest implements LLMRequest for the Anthropic Messages API.
type anthropicLLMRequest struct {
	model  string
	stream bool
}

func (r *anthropicLLMRequest) GetModel() string { return r.model }
func (r *anthropicLLMRequest) IsStream() bool   { return r.stream }

// parseAnthropicRequest parses an Anthropic Messages API request body and returns
// an LLMRequest with the extracted model and stream fields.
func parseAnthropicRequest(body []byte) (LLMRequest, error) {
	var req anthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &anthropicLLMRequest{model: req.Model, stream: req.Stream}, nil
}

// --- LLMResponse implementation ---

// anthropicLLMResponse implements LLMResponse for the Anthropic Messages API.
type anthropicLLMResponse struct {
	usage LLMUsage
}

func (r *anthropicLLMResponse) GetUsage() LLMUsage { return r.usage }

// parseAnthropicResponse parses an Anthropic Messages API response body and returns
// an LLMResponse with the extracted token-usage information.
func parseAnthropicResponse(body []byte) (LLMResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &anthropicLLMResponse{usage: anthropicUsageToLLM(resp.Usage)}, nil
}

// --- LLMResponseChunk implementation ---

// anthropicLLMResponseChunk implements LLMResponseChunk for the Anthropic streaming API.
type anthropicLLMResponseChunk struct {
	usage LLMUsage
}

func (c *anthropicLLMResponseChunk) GetUsage() LLMUsage { return c.usage }

// parseAnthropicChunk parses a single Anthropic SSE event and returns an
// LLMResponseChunk containing any usage data carried by that event.
// eventType is the value from the preceding "event:" SSE line.
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
	}
	return anthropicLLMResponseChunk{}, nil
}

// anthropicUsageToLLM converts an anthropicUsage to an LLMUsage.
// Returns the zero value when u is nil.
func anthropicUsageToLLM(u anthropicUsage) LLMUsage {
	return LLMUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.InputTokens + u.OutputTokens,
	}
}

// --- SSE accumulator ---

var (
	anthropicSSEEventPrefix = []byte("event: ")
	anthropicSSEDataPrefix  = []byte("data: ")
)

// anthropicSSEParser accumulates usage information from an Anthropic streaming SSE response.
// It consumes body chunks as they arrive and produces an LLMResponse when finished.
type anthropicSSEParser struct {
	buf          []byte
	done         bool
	inputTokens  uint32
	outputTokens uint32
	// currentEvent tracks the most recently seen "event:" value so that the
	// following "data:" line can be routed to the correct handler.
	currentEvent string
}

func newAnthropicSSEParser() *anthropicSSEParser {
	return &anthropicSSEParser{}
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

func (a *anthropicSSEParser) processEvent(eventType string, data []byte) error {
	if eventType == "message_stop" {
		a.done = true
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
	return nil
}

func (a *anthropicSSEParser) Finish() (LLMResponse, error) {
	return &anthropicLLMResponse{usage: LLMUsage{
		InputTokens:  a.inputTokens,
		OutputTokens: a.outputTokens,
		TotalTokens:  a.inputTokens + a.outputTokens,
	}}, nil
}

// anthropicFactory implements LLMFactory for the Anthropic Messages API.
type anthropicFactory struct{}

func (f *anthropicFactory) ParseRequest(body []byte) (LLMRequest, error) {
	return parseAnthropicRequest(body)
}

func (f *anthropicFactory) ParseResponse(body []byte) (LLMResponse, error) {
	return parseAnthropicResponse(body)
}

func (f *anthropicFactory) NewSSEParser() SSEParser { return newAnthropicSSEParser() }
