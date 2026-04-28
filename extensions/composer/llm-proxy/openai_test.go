// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// --- parseOpenAIRequest ---

func TestParseOpenAIRequest_Basic(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","stream":false}`)
	req, err := parseOpenAIRequest(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4o", req.GetModel())
	require.False(t, req.IsStream())
}

func TestParseOpenAIRequest_Stream(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","stream":true}`)
	req, err := parseOpenAIRequest(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4o-mini", req.GetModel())
	require.True(t, req.IsStream())
}

func TestParseOpenAIRequest_MissingFields(t *testing.T) {
	body := []byte(`{}`)
	req, err := parseOpenAIRequest(body)
	require.NoError(t, err)
	require.Empty(t, req.GetModel())
	require.False(t, req.IsStream())
}

func TestParseOpenAIRequest_InvalidJSON(t *testing.T) {
	_, err := parseOpenAIRequest([]byte(`{invalid`))
	require.Error(t, err)
}

// --- parseOpenAIResponse ---

func TestParseOpenAIResponse_WithUsage(t *testing.T) {
	body := []byte(`{
		"choices":[],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`)
	resp, err := parseOpenAIResponse(body)
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(10), usage.InputTokens)
	require.Equal(t, uint32(20), usage.OutputTokens)
	require.Equal(t, uint32(30), usage.TotalTokens)
}

func TestParseOpenAIResponse_NoUsage(t *testing.T) {
	body := []byte(`{"choices":[]}`)
	resp, err := parseOpenAIResponse(body)
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, resp.GetUsage())
}

func TestParseOpenAIResponse_InvalidJSON(t *testing.T) {
	_, err := parseOpenAIResponse([]byte(`{bad}`))
	require.Error(t, err)
}

// --- parseOpenAIChunk ---

func TestParseOpenAIChunk_WithUsage(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":5,"completion_tokens":15,"total_tokens":20}}`)
	chunk, err := parseOpenAIChunk(data)
	require.NoError(t, err)
	usage := chunk.GetUsage()
	require.Equal(t, uint32(5), usage.InputTokens)
	require.Equal(t, uint32(15), usage.OutputTokens)
	require.Equal(t, uint32(20), usage.TotalTokens)
}

func TestParseOpenAIChunk_NoUsage(t *testing.T) {
	data := []byte(`{"choices":[{"delta":{"content":"hello"}}]}`)
	chunk, err := parseOpenAIChunk(data)
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, chunk.GetUsage())
}

func TestParseOpenAIChunk_InvalidJSON(t *testing.T) {
	_, err := parseOpenAIChunk([]byte(`bad`))
	require.Error(t, err)
}

// --- openaiSSEAccumulator ---

func TestOpenAISSEAccumulator_NoUsage(t *testing.T) {
	acc := newOpenAISSEParser()
	require.NoError(t, acc.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n")))
	require.NoError(t, acc.Feed([]byte("data: [DONE]\n")))
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, resp.GetUsage())
}

func TestOpenAISSEAccumulator_WithUsage(t *testing.T) {
	acc := newOpenAISSEParser()
	require.NoError(t, acc.Feed([]byte("data: {\"choices\":[]}\n")))
	require.NoError(t, acc.Feed([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n")))
	require.NoError(t, acc.Feed([]byte("data: [DONE]\n")))
	resp, err := acc.Finish()
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(8), usage.InputTokens)
	require.Equal(t, uint32(4), usage.OutputTokens)
	require.Equal(t, uint32(12), usage.TotalTokens)
}

func TestOpenAISSEAccumulator_ChunkedFeed(t *testing.T) {
	acc := newOpenAISSEParser()
	// Feed partial lines across multiple calls.
	raw := "data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":7,\"total_tokens\":10}}\ndata: [DONE]\n"
	for _, b := range []byte(raw) {
		require.NoError(t, acc.Feed([]byte{b}))
	}
	resp, err := acc.Finish()
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(3), usage.InputTokens)
	require.Equal(t, uint32(7), usage.OutputTokens)
	require.Equal(t, uint32(10), usage.TotalTokens)
}

func TestOpenAISSEAccumulator_IgnoresAfterDone(t *testing.T) {
	acc := newOpenAISSEParser()
	require.NoError(t, acc.Feed([]byte("data: [DONE]\n")))
	// Feed after done should be ignored.
	require.NoError(t, acc.Feed([]byte("data: {\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n")))
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, resp.GetUsage())
}
