// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openai

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

func TestParseOpenAIRequest_Messages(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[
			{"role":"system","content":"You are helpful."},
			{"role":"user","content":"Hello"}
		]
	}`)
	req, err := parseOpenAIRequest(body)
	require.NoError(t, err)
	msgs := req.GetMessages()
	require.Len(t, msgs, 2)
	require.Equal(t, "system", msgs[0].Role)
	require.Equal(t, "You are helpful.", msgs[0].Content[0].Text)
	require.Equal(t, "user", msgs[1].Role)
	require.Equal(t, "Hello", msgs[1].Content[0].Text)
}

func TestParseOpenAIRequest_ToolCalls(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}
			]}
		],
		"tools":[
			{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object"}}}
		],
		"tool_choice":"auto"
	}`)
	req, err := parseOpenAIRequest(body)
	require.NoError(t, err)

	msgs := req.GetMessages()
	require.Len(t, msgs, 1)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Len(t, msgs[0].ToolCalls, 1)
	require.Equal(t, "call_1", msgs[0].ToolCalls[0].ID)
	require.Equal(t, "get_weather", msgs[0].ToolCalls[0].Name)

	tools := req.GetTools()
	require.Len(t, tools, 1)
	require.Equal(t, "function", tools[0].Type)
	require.Equal(t, "get_weather", tools[0].Name)
	require.Equal(t, "Get weather", tools[0].Description)

	require.Equal(t, &llm.LLMToolChoice{Type: "auto"}, req.GetToolChoice())
}

func TestParseOpenAIRequest_ToJSON(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}],"stream":true}`)
	req, err := parseOpenAIRequest(body)
	require.NoError(t, err)
	out, err := req.ToJSON()
	require.NoError(t, err)
	// Round-trip: model and stream must survive serialisation.
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "gpt-4o", m["model"])
	require.Equal(t, true, m["stream"])
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
	require.Equal(t, llm.LLMUsage{}, resp.GetUsage())
}

func TestParseOpenAIResponse_InvalidJSON(t *testing.T) {
	_, err := parseOpenAIResponse([]byte(`{bad}`))
	require.Error(t, err)
}

func TestParseOpenAIResponse_AllFields(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl-123","model":"gpt-4o",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"Hello!"},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`)
	resp, err := parseOpenAIResponse(body)
	require.NoError(t, err)
	require.Equal(t, "chatcmpl-123", resp.GetID())
	require.Equal(t, "gpt-4o", resp.GetModel())
	msgs := resp.GetMessages()
	require.Len(t, msgs, 1)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Equal(t, "Hello!", msgs[0].Content[0].Text)
	require.Equal(t, "stop", resp.GetStopReason())

	out, err := resp.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "chatcmpl-123", m["id"])
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
	require.Equal(t, llm.LLMUsage{}, chunk.GetUsage())
}

func TestParseOpenAIChunk_InvalidJSON(t *testing.T) {
	_, err := parseOpenAIChunk([]byte(`bad`))
	require.Error(t, err)
}

func TestParseOpenAIChunk_AllFields(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl-abc","model":"gpt-4o",
		"choices":[{
			"index":0,
			"delta":{"role":"assistant","content":"Hi"},
			"finish_reason":null
		}]
	}`)
	chunk, err := parseOpenAIChunk(body)
	require.NoError(t, err)
	require.Equal(t, "chatcmpl-abc", chunk.GetID())
	require.Equal(t, "gpt-4o", chunk.GetModel())
	require.Equal(t, "", chunk.GetStopReason())
	msg := chunk.GetMessages()
	require.Len(t, msg, 1)
	require.Equal(t, "assistant", msg[0].Role)
	require.Equal(t, "Hi", msg[0].Content[0].Text)

	out, err := chunk.ToEvent()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(sseDataJSON(out), &m))
	require.Equal(t, "chatcmpl-abc", m["id"])
}

// --- openaiChatSSEAccumulator ---

func TestOpenAISSEAccumulator_NoUsage(t *testing.T) {
	acc := newOpenAISSEParser()
	_, err := acc.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n"))
	require.NoError(t, err)
	_, err = acc.Feed([]byte("data: [DONE]\n"))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, resp.GetUsage())
}

func TestOpenAISSEAccumulator_WithUsage(t *testing.T) {
	acc := newOpenAISSEParser()
	_, err := acc.Feed([]byte("data: {\"choices\":[]}\n"))
	require.NoError(t, err)
	_, err = acc.Feed([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n"))
	require.NoError(t, err)
	_, err = acc.Feed([]byte("data: [DONE]\n"))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
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
		_, err := acc.Feed([]byte{b})
		require.NoError(t, err)
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
	_, err := acc.Feed([]byte("data: [DONE]\n"))
	require.NoError(t, err)
	// Feed after done should be ignored.
	_, err = acc.Feed([]byte("data: {\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n"))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.NoError(t, err)
	require.Equal(t, llm.LLMUsage{}, resp.GetUsage())
}

func TestOpenAISSEAccumulator_TracksIDAndModel(t *testing.T) {
	acc := newOpenAISSEParser()
	_, err := acc.Feed([]byte("data: {\"id\":\"chatcmpl-xyz\",\"model\":\"gpt-4o\",\"choices\":[]}\n"))
	require.NoError(t, err)
	_, err = acc.Feed([]byte("data: [DONE]\n"))
	require.NoError(t, err)
	resp, err := acc.Finish()
	require.NoError(t, err)
	require.NoError(t, err)
	require.Equal(t, "chatcmpl-xyz", resp.GetID())
	require.Equal(t, "gpt-4o", resp.GetModel())
}

