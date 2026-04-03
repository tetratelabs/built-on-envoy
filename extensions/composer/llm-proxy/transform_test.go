// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	anthropicpkg "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/anthropic"
	openaipkg "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/openai"
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

// --- Transform: OpenAI → Anthropic ---

func TestTransformRequest_OpenAIToAnthropic(t *testing.T) {
	body := []byte(`{
"model":"gpt-4o",
"messages":[
{"role":"system","content":"Be concise."},
{"role":"user","content":"Hello"}
],
"stream":true,
"max_tokens":512,
"temperature":0.7
}`)
	req, err := openaipkg.NewFactory().ParseRequest(body)
	require.NoError(t, err)

	f := anthropicpkg.NewFactory()
	transformed, err := f.TransformRequest(req)
	require.NoError(t, err)
	require.Equal(t, "gpt-4o", transformed.GetModel())
	require.True(t, transformed.IsStream())

	msgs := transformed.GetMessages()
	require.NotEmpty(t, msgs)
	require.Equal(t, "system", msgs[0].Role)

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "gpt-4o", m["model"])
	require.Equal(t, "Be concise.", m["system"])
	require.Equal(t, true, m["stream"])
	require.EqualValues(t, 512, m["max_tokens"])
}

func TestTransformRequest_WithTools_OpenAIToAnthropic(t *testing.T) {
	body := []byte(`{
"model":"gpt-4o",
"messages":[{"role":"user","content":"What's the weather?"}],
"tools":[{
"type":"function",
"function":{
"name":"get_weather",
"description":"Returns weather",
"parameters":{"type":"object","properties":{"city":{"type":"string"}}}
}
}],
"tool_choice":"auto"
}`)
	req, err := openaipkg.NewFactory().ParseRequest(body)
	require.NoError(t, err)

	f := anthropicpkg.NewFactory()
	transformed, err := f.TransformRequest(req)
	require.NoError(t, err)

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))

	tools := m["tools"].([]interface{})
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]interface{})
	require.Equal(t, "get_weather", tool["name"])
	require.NotNil(t, tool["input_schema"])
	require.Nil(t, tool["parameters"])
}

func TestTransformRequest_ToolCalls_OpenAIToAnthropic(t *testing.T) {
	body := []byte(`{
"model":"gpt-4o",
"messages":[
{"role":"user","content":"What's the weather?"},
{"role":"assistant","content":null,"tool_calls":[
{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}
]},
{"role":"tool","tool_call_id":"call_1","content":"Sunny, 72°F"}
]
}`)
	req, err := openaipkg.NewFactory().ParseRequest(body)
	require.NoError(t, err)

	f := anthropicpkg.NewFactory()
	transformed, err := f.TransformRequest(req)
	require.NoError(t, err)

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))

	messages := m["messages"].([]interface{})
	require.Len(t, messages, 3)

	assistantMsg := messages[1].(map[string]interface{})
	require.Equal(t, "assistant", assistantMsg["role"])
	blocks := assistantMsg["content"].([]interface{})
	require.Len(t, blocks, 1)
	block := blocks[0].(map[string]interface{})
	require.Equal(t, "tool_use", block["type"])
	require.Equal(t, "get_weather", block["name"])
}

func TestTransformResponse_OpenAIToAnthropic(t *testing.T) {
	body := []byte(`{
"id":"chatcmpl-123","model":"gpt-4o",
"choices":[{
"index":0,
"message":{"role":"assistant","content":"Hello there!"},
"finish_reason":"stop"
}],
"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
}`)
	resp, err := openaipkg.NewFactory().ParseResponse(body)
	require.NoError(t, err)

	f := anthropicpkg.NewFactory()
	transformed, err := f.TransformResponse(resp)
	require.NoError(t, err)
	require.Equal(t, "chatcmpl-123", transformed.GetID())
	require.Equal(t, "gpt-4o", transformed.GetModel())
	require.Equal(t, uint32(10), transformed.GetUsage().InputTokens)

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "end_turn", m["stop_reason"])
	content := m["content"].([]interface{})
	require.Len(t, content, 1)
	require.Equal(t, "Hello there!", content[0].(map[string]interface{})["text"])
}

func TestTransformChunk_OpenAIToAnthropic_ContentDelta(t *testing.T) {
	data := []byte(`{"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)
	chunk, err := openaipkg.NewFactory().ParseChunk(data)
	require.NoError(t, err)

	f := anthropicpkg.NewFactory()
	transformed, err := f.TransformChunk(chunk)
	require.NoError(t, err)
	require.Equal(t, "chatcmpl-1", transformed.GetID())

	msgs := transformed.GetMessages()
	require.Len(t, msgs, 1)
	require.Equal(t, "Hello", msgs[0].Content[0].Text)

	out, err := transformed.ToEvent()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(sseDataJSON(out), &m))
	require.Equal(t, "content_block_delta", m["type"])
}

func TestTransformChunk_OpenAIToAnthropic_FinishReason(t *testing.T) {
	data := []byte(`{"id":"chatcmpl-2","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	chunk, err := openaipkg.NewFactory().ParseChunk(data)
	require.NoError(t, err)

	f := anthropicpkg.NewFactory()
	transformed, err := f.TransformChunk(chunk)
	require.NoError(t, err)
	require.Equal(t, "stop", transformed.GetStopReason())

	out, err := transformed.ToEvent()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(sseDataJSON(out), &m))
	require.Equal(t, "message_delta", m["type"])
}

// --- Transform: Anthropic → OpenAI ---

func TestTransformRequest_AnthropicToOpenAI(t *testing.T) {
	body := []byte(`{
"model":"claude-3-5-sonnet-20241022",
"system":"Be concise.",
"messages":[{"role":"user","content":"Hello"}],
"max_tokens":256
}`)
	req, err := anthropicpkg.NewFactory().ParseRequest(body)
	require.NoError(t, err)

	f := openaipkg.NewFactory()
	transformed, err := f.TransformRequest(req)
	require.NoError(t, err)
	require.Equal(t, "claude-3-5-sonnet-20241022", transformed.GetModel())

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "claude-3-5-sonnet-20241022", m["model"])
	messages := m["messages"].([]interface{})
	require.NotEmpty(t, messages)
	first := messages[0].(map[string]interface{})
	require.Equal(t, "system", first["role"])
	require.Equal(t, "Be concise.", first["content"])
}

func TestTransformRequest_WithTools_AnthropicToOpenAI(t *testing.T) {
	body := []byte(`{
"model":"claude-3-5-sonnet-20241022",
"messages":[{"role":"user","content":"Use the tool"}],
"tools":[{
"name":"get_weather",
"description":"Returns weather",
"input_schema":{"type":"object","properties":{"city":{"type":"string"}}}
}],
"max_tokens":256
}`)
	req, err := anthropicpkg.NewFactory().ParseRequest(body)
	require.NoError(t, err)

	f := openaipkg.NewFactory()
	transformed, err := f.TransformRequest(req)
	require.NoError(t, err)

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))

	tools := m["tools"].([]interface{})
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]interface{})
	fn := tool["function"].(map[string]interface{})
	require.Equal(t, "get_weather", fn["name"])
	require.NotNil(t, fn["parameters"])
	require.Nil(t, fn["input_schema"])
}

func TestTransformResponse_AnthropicToOpenAI(t *testing.T) {
	body := []byte(`{
"id":"msg_abc","model":"claude-3-5-sonnet-20241022","role":"assistant",
"content":[{"type":"text","text":"Hi!"}],
"stop_reason":"end_turn",
"usage":{"input_tokens":5,"output_tokens":3}
}`)
	resp, err := anthropicpkg.NewFactory().ParseResponse(body)
	require.NoError(t, err)

	f := openaipkg.NewFactory()
	transformed, err := f.TransformResponse(resp)
	require.NoError(t, err)

	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "msg_abc", m["id"])
	choices := m["choices"].([]interface{})
	require.Len(t, choices, 1)
	choice := choices[0].(map[string]interface{})
	require.Equal(t, "stop", choice["finish_reason"])
	msg := choice["message"].(map[string]interface{})
	require.Equal(t, "Hi!", msg["content"])
}

func TestTransformChunk_AnthropicToOpenAI_ContentDelta(t *testing.T) {
	data := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`)
	chunk, err := anthropicpkg.NewFactory().ParseChunk(data)
	require.NoError(t, err)

	f := openaipkg.NewFactory()
	transformed, err := f.TransformChunk(chunk)
	require.NoError(t, err)

	out, err := transformed.ToEvent()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(sseDataJSON(out), &m))
	choices := m["choices"].([]interface{})
	require.Len(t, choices, 1)
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	require.Equal(t, "Hi", delta["content"])
}

func TestTransformRequest_UnsupportedKind(t *testing.T) {
	req, err := openaipkg.NewFactory().ParseRequest([]byte(`{"model":"gpt-4o","messages":[]}`))
	require.NoError(t, err)
	_, err = TransformLLMRequestTo(req, "unknown")
	require.Error(t, err)
}

func TestTransformResponse_UnsupportedKind(t *testing.T) {
	resp, err := openaipkg.NewFactory().ParseResponse([]byte(`{"choices":[],"usage":{}}`))
	require.NoError(t, err)
	_, err = TransformLLMResponseTo(resp, "unknown")
	require.Error(t, err)
}

func TestTransformChunk_UnsupportedKind(t *testing.T) {
	chunk, err := openaipkg.NewFactory().ParseChunk([]byte(`{"choices":[]}`))
	require.NoError(t, err)
	_, err = TransformLLMResponseChunkTo(chunk, "unknown")
	require.Error(t, err)
}

// --- Transform: Custom (same as OpenAI) ---

func TestTransformRequest_CustomSameAsOpenAI(t *testing.T) {
	body := []byte(`{"model":"my-model","messages":[{"role":"user","content":"hi"}]}`)
	req, err := openaipkg.NewFactory().ParseRequest(body)
	require.NoError(t, err)

	transformed, err := TransformLLMRequestTo(req, KindCustom)
	require.NoError(t, err)
	out, err := transformed.ToJSON()
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "my-model", m["model"])
}
