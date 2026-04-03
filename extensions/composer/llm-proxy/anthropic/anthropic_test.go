// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	llm "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/llm"
)

// sseDataJSON extracts the JSON payload from an SSE event ("data: <json>\n\n").
func sseDataJSON(event []byte) []byte {
	for _, line := range strings.Split(string(event), "\n") {
		if strings.HasPrefix(line, "data: ") {
			return []byte(line[6:])
		}
	}
	return nil
}

// --- parseRequest ---

func TestParseAnthropicRequest_Basic(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-sonnet-20241022","stream":false}`)
	req, err := parseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "claude-3-5-sonnet-20241022", req.GetModel())
	require.False(t, req.IsStream())
}

func TestParseAnthropicRequest_Stream(t *testing.T) {
	body := []byte(`{"model":"claude-3-haiku-20240307","stream":true}`)
	req, err := parseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "claude-3-haiku-20240307", req.GetModel())
	require.True(t, req.IsStream())
}

func TestParseAnthropicRequest_MissingFields(t *testing.T) {
	body := []byte(`{}`)
	req, err := parseRequest(body)
	require.NoError(t, err)
	require.Empty(t, req.GetModel())
	require.False(t, req.IsStream())
}

func TestParseAnthropicRequest_InvalidJSON(t *testing.T) {
	_, err := parseRequest([]byte(`{invalid`))
	require.Error(t, err)
}

func TestParseAnthropicRequest_SystemAndMessages(t *testing.T) {
	body := []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"system":"You are a helpful assistant.",
		"messages":[
			{"role":"user","content":"Hello"},
			{"role":"assistant","content":"Hi there!"}
		],
		"max_tokens":1024
	}`)
	req, err := parseRequest(body)
	require.NoError(t, err)

	// GetMessages() should prepend the system prompt as a "system" role message.
	msgs := req.GetMessages()
	require.Len(t, msgs, 3)
	require.Equal(t, "system", msgs[0].Role)
	require.Equal(t, "You are a helpful assistant.", msgs[0].Content[0].Text)
	require.Equal(t, "user", msgs[1].Role)
	require.Equal(t, "Hello", msgs[1].Content[0].Text)
	require.Equal(t, "assistant", msgs[2].Role)
	require.Equal(t, "Hi there!", msgs[2].Content[0].Text)

	mt := req.GetMaxTokens()
	require.NotNil(t, mt)
	require.Equal(t, 1024, *mt)
}

func TestParseAnthropicRequest_Tools(t *testing.T) {
	body := []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"messages":[{"role":"user","content":"Use the tool"}],
		"tools":[{
			"name":"get_weather",
			"description":"Get current weather",
			"input_schema":{"type":"object","properties":{"city":{"type":"string"}}}
		}],
		"max_tokens":256
	}`)
	req, err := parseRequest(body)
	require.NoError(t, err)
	tools := req.GetTools()
	require.Len(t, tools, 1)
	require.Equal(t, "function", tools[0].Type)
	require.Equal(t, "get_weather", tools[0].Name)
	require.Equal(t, "Get current weather", tools[0].Description)
	// input_schema should be exposed as Parameters and marshal to valid JSON.
	rawParams, err := json.Marshal(tools[0].Parameters)
	require.NoError(t, err)
	require.True(t, json.Valid(rawParams))
}

func TestParseAnthropicRequest_ToJSON(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"Hi"}],"stream":true,"max_tokens":512}`)
	req, err := parseRequest(body)
	require.NoError(t, err)
	out, err := req.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "claude-3-5-sonnet-20241022", m["model"])
	require.Equal(t, true, m["stream"])
}

// --- parseResponse ---

func TestParseAnthropicResponse_WithUsage(t *testing.T) {
	body := []byte(`{
		"id":"msg_01","type":"message","role":"assistant","content":[],
		"usage":{"input_tokens":12,"output_tokens":34}
	}`)
	resp, err := parseResponse(body)
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(12), usage.InputTokens)
	require.Equal(t, uint32(34), usage.OutputTokens)
	require.Equal(t, uint32(46), usage.TotalTokens)
}

func TestParseAnthropicResponse_NoUsage(t *testing.T) {
	body := []byte(`{"id":"msg_01","type":"message"}`)
	resp, err := parseResponse(body)
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, resp.GetUsage())
}

func TestParseAnthropicResponse_InvalidJSON(t *testing.T) {
	_, err := parseResponse([]byte(`bad`))
	require.Error(t, err)
}

func TestParseAnthropicResponse_AllFields(t *testing.T) {
	body := []byte(`{
		"id":"msg_abc","model":"claude-3-5-sonnet-20241022","role":"assistant",
		"content":[{"type":"text","text":"Hello!"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":5,"output_tokens":3}
	}`)
	resp, err := parseResponse(body)
	require.NoError(t, err)
	require.Equal(t, "msg_abc", resp.GetID())
	require.Equal(t, "claude-3-5-sonnet-20241022", resp.GetModel())
	msgs := resp.GetMessages()
	require.Len(t, msgs, 1)
	require.Equal(t, "Hello!", msgs[0].Content[0].Text)
	require.Equal(t, "stop", resp.GetStopReason()) // end_turn → stop

	out, err := resp.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "msg_abc", m["id"])
}

// --- parseChunk ---

func TestParseAnthropicChunk_MessageStart(t *testing.T) {
	data := []byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude-3-5-sonnet","role":"assistant","usage":{"input_tokens":20,"output_tokens":0}}}`)
	chunk, err := parseChunk("message_start", data)
	require.NoError(t, err)
	usage := chunk.GetUsage()
	require.Equal(t, uint32(20), usage.InputTokens)
	require.Equal(t, uint32(0), usage.OutputTokens)
	require.Equal(t, uint32(20), usage.TotalTokens)
	require.Equal(t, "msg_1", chunk.GetID())
	require.Equal(t, "claude-3-5-sonnet", chunk.GetModel())
}

func TestParseAnthropicChunk_MessageDelta(t *testing.T) {
	data := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}`)
	chunk, err := parseChunk("message_delta", data)
	require.NoError(t, err)
	usage := chunk.GetUsage()
	require.Equal(t, uint32(0), usage.InputTokens)
	require.Equal(t, uint32(15), usage.OutputTokens)
	require.Equal(t, "stop", chunk.GetStopReason()) // end_turn → stop
}

func TestParseAnthropicChunk_ContentBlockDelta_NoUsage(t *testing.T) {
	data := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)
	chunk, err := parseChunk("content_block_delta", data)
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, chunk.GetUsage())
	msg := chunk.GetMessages()
	require.Len(t, msg, 1)
	require.Equal(t, "hi", msg[0].Content[0].Text)
}

func TestParseAnthropicChunk_UnknownEvent_NoUsage(t *testing.T) {
	chunk, err := parseChunk("ping", []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, chunk.GetUsage())
}

func TestParseAnthropicChunk_MessageStart_InvalidJSON(t *testing.T) {
	_, err := parseChunk("message_start", []byte(`bad`))
	require.Error(t, err)
}

func TestParseAnthropicChunk_ToEvent_ContentBlockDelta(t *testing.T) {
	data := []byte(`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"hello"}}`)
	chunk, err := parseChunk("content_block_delta", data)
	require.NoError(t, err)
	out, err := chunk.ToEvent()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(out), "event: content_block_delta\n"))
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(sseDataJSON(out), &m))
	require.Equal(t, "content_block_delta", m["type"])
}

// --- sseParser ---

func TestAnthropicSSEParser_FullStream(t *testing.T) {
	acc := newSSEParser()

	events := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-3\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":25,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	_, err := acc.Feed([]byte(events))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(25), usage.InputTokens)
	require.Equal(t, uint32(10), usage.OutputTokens)
	require.Equal(t, uint32(35), usage.TotalTokens)
	require.Equal(t, "msg_1", resp.GetID())
	require.Equal(t, "claude-3", resp.GetModel())
	msgs := resp.GetMessages()
	require.Len(t, msgs, 1)
	require.Equal(t, "Hello", msgs[0].Content[0].Text)
}

func TestAnthropicSSEParser_NoUsageEvents(t *testing.T) {
	acc := newSSEParser()
	_, err := acc.Feed([]byte("event: ping\ndata: {}\n\n"))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, resp.GetUsage())
}

func TestAnthropicSSEParser_ChunkedFeed(t *testing.T) {
	acc := newSSEParser()
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"\",\"model\":\"\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n"
	for _, b := range []byte(raw) {
		_, err := acc.Feed([]byte{b})
	require.NoError(t, err)
	}
	resp, err := acc.Finish()
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(7), usage.InputTokens)
	require.Equal(t, uint32(3), usage.OutputTokens)
	require.Equal(t, uint32(10), usage.TotalTokens)
}

func TestAnthropicSSEParser_IgnoresAfterDone(t *testing.T) {
	acc := newSSEParser()
	_, err := acc.Feed([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	require.NoError(t, err)
	// Feeding more data after message_stop should be ignored.
	_, err = acc.Feed([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"\",\"model\":\"\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":99,\"output_tokens\":0}}}\n\n"))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, resp.GetUsage())
}

