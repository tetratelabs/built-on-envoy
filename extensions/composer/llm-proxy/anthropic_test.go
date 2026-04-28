// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// --- parseAnthropicRequest ---

func TestParseAnthropicRequest_Basic(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-sonnet-20241022","stream":false}`)
	req, err := parseAnthropicRequest(body)
	require.NoError(t, err)
	require.Equal(t, "claude-3-5-sonnet-20241022", req.GetModel())
	require.False(t, req.IsStream())
}

func TestParseAnthropicRequest_Stream(t *testing.T) {
	body := []byte(`{"model":"claude-3-haiku-20240307","stream":true}`)
	req, err := parseAnthropicRequest(body)
	require.NoError(t, err)
	require.Equal(t, "claude-3-haiku-20240307", req.GetModel())
	require.True(t, req.IsStream())
}

func TestParseAnthropicRequest_MissingFields(t *testing.T) {
	body := []byte(`{}`)
	req, err := parseAnthropicRequest(body)
	require.NoError(t, err)
	require.Empty(t, req.GetModel())
	require.False(t, req.IsStream())
}

func TestParseAnthropicRequest_InvalidJSON(t *testing.T) {
	_, err := parseAnthropicRequest([]byte(`{invalid`))
	require.Error(t, err)
}

// --- parseAnthropicResponse ---

func TestParseAnthropicResponse_WithUsage(t *testing.T) {
	body := []byte(`{
		"id":"msg_01","type":"message","role":"assistant","content":[],
		"usage":{"input_tokens":12,"output_tokens":34}
	}`)
	resp, err := parseAnthropicResponse(body)
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(12), usage.InputTokens)
	require.Equal(t, uint32(34), usage.OutputTokens)
	require.Equal(t, uint32(46), usage.TotalTokens)
}

func TestParseAnthropicResponse_NoUsage(t *testing.T) {
	body := []byte(`{"id":"msg_01","type":"message"}`)
	resp, err := parseAnthropicResponse(body)
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, resp.GetUsage())
}

func TestParseAnthropicResponse_InvalidJSON(t *testing.T) {
	_, err := parseAnthropicResponse([]byte(`bad`))
	require.Error(t, err)
}

// --- parseAnthropicChunk ---

func TestParseAnthropicChunk_MessageStart(t *testing.T) {
	data := []byte(`{"type":"message_start","message":{"usage":{"input_tokens":20,"output_tokens":0}}}`)
	chunk, err := parseAnthropicChunk("message_start", data)
	require.NoError(t, err)
	usage := chunk.GetUsage()
	require.Equal(t, uint32(20), usage.InputTokens)
	require.Equal(t, uint32(0), usage.OutputTokens)
	require.Equal(t, uint32(20), usage.TotalTokens)
}

func TestParseAnthropicChunk_MessageDelta(t *testing.T) {
	data := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}`)
	chunk, err := parseAnthropicChunk("message_delta", data)
	require.NoError(t, err)
	usage := chunk.GetUsage()
	require.Equal(t, uint32(0), usage.InputTokens)
	require.Equal(t, uint32(15), usage.OutputTokens)
}

func TestParseAnthropicChunk_ContentBlockDelta_NoUsage(t *testing.T) {
	data := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)
	chunk, err := parseAnthropicChunk("content_block_delta", data)
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, chunk.GetUsage())
}

func TestParseAnthropicChunk_UnknownEvent_NoUsage(t *testing.T) {
	chunk, err := parseAnthropicChunk("ping", []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, chunk.GetUsage())
}

func TestParseAnthropicChunk_MessageStart_InvalidJSON(t *testing.T) {
	_, err := parseAnthropicChunk("message_start", []byte(`bad`))
	require.Error(t, err)
}

// --- anthropicSSEParser ---

func TestAnthropicSSEParser_FullStream(t *testing.T) {
	acc := newAnthropicSSEParser()

	events := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":25,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	require.NoError(t, acc.Feed([]byte(events)))
	resp, err := acc.Finish()
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(25), usage.InputTokens)
	require.Equal(t, uint32(10), usage.OutputTokens)
	require.Equal(t, uint32(35), usage.TotalTokens)
}

func TestAnthropicSSEParser_NoUsageEvents(t *testing.T) {
	acc := newAnthropicSSEParser()
	require.NoError(t, acc.Feed([]byte("event: ping\ndata: {}\n\n")))
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, resp.GetUsage())
}

func TestAnthropicSSEParser_ChunkedFeed(t *testing.T) {
	acc := newAnthropicSSEParser()
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":3}}\n\n"
	for _, b := range []byte(raw) {
		require.NoError(t, acc.Feed([]byte{b}))
	}
	resp, err := acc.Finish()
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(7), usage.InputTokens)
	require.Equal(t, uint32(3), usage.OutputTokens)
	require.Equal(t, uint32(10), usage.TotalTokens)
}

func TestAnthropicSSEParser_IgnoresAfterDone(t *testing.T) {
	acc := newAnthropicSSEParser()
	require.NoError(t, acc.Feed([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")))
	// Feeding more data after message_stop should be ignored.
	require.NoError(t, acc.Feed([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":99,\"output_tokens\":0}}}\n\n")))
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.Equal(t, LLMUsage{}, resp.GetUsage())
}
