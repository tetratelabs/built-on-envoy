// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Tests for decodeAnthropicRequest ---

func TestDecodeAnthropicRequest_SimpleMessages(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Hello, how are you?"}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-20250514", result.Model)
	require.Len(t, result.Messages, 1)
	require.Equal(t, "user", result.Messages[0].Role)
	require.Equal(t, "Hello, how are you?", extractAnthropicContent(result.Messages[0].Content))
	require.Empty(t, result.Tools)
}

func TestDecodeAnthropicRequest_WithStringSystem(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"system": "You are a helpful assistant.",
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Equal(t, "You are a helpful assistant.", extractAnthropicSystem(result.System))
	require.Len(t, result.Messages, 1)
}

func TestDecodeAnthropicRequest_WithArraySystem(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"system": [
			{"type": "text", "text": "First instruction"},
			{"type": "text", "text": "Second instruction"}
		],
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First instruction\nSecond instruction", extractAnthropicSystem(result.System))
}

func TestDecodeAnthropicRequest_MultiTurnConversation(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 3)
	require.Equal(t, "user", result.Messages[0].Role)
	require.Equal(t, "First question", extractAnthropicContent(result.Messages[0].Content))
	require.Equal(t, "assistant", result.Messages[1].Role)
	require.Equal(t, "First answer", extractAnthropicContent(result.Messages[1].Content))
	require.Equal(t, "user", result.Messages[2].Role)
	require.Equal(t, "Second question", extractAnthropicContent(result.Messages[2].Content))
}

func TestDecodeAnthropicRequest_WithContentBlockArray(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What is in this image?"},
				{"type": "image", "source": {"type": "base64", "data": "..."}}
			]}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	require.Equal(t, "What is in this image?", extractAnthropicContent(result.Messages[0].Content))
}

func TestDecodeAnthropicRequest_WithTools(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "What is the weather?"}
		],
		"tools": [
			{"name": "get_weather", "description": "Get weather info", "input_schema": {"type": "object"}},
			{"name": "send_email", "description": "Send an email", "input_schema": {"type": "object"}}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)
	require.Equal(t, "get_weather", result.Tools[0].Name)
	require.Equal(t, "send_email", result.Tools[1].Name)
}

func TestDecodeAnthropicRequest_WithToolUse(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Let me check."},
				{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"location": "NYC"}}
			]}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 2)
	require.Equal(t, "assistant", result.Messages[1].Role)
	toolCalls := extractAnthropicToolCalls(result.Messages[1].Content)
	require.Len(t, toolCalls, 1)
	require.Equal(t, "toolu_1", toolCalls[0].ID)
	require.Equal(t, "get_weather", toolCalls[0].Name)
	require.JSONEq(t, `{"location": "NYC"}`, string(toolCalls[0].Input))
}

func TestDecodeAnthropicRequest_EmptyMessages(t *testing.T) {
	body := []byte(`{"model": "claude-sonnet-4-20250514", "messages": []}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-20250514", result.Model)
	require.Empty(t, result.Messages)
	require.Empty(t, result.Tools)
}

func TestDecodeAnthropicRequest_NoModel(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)

	result, err := decodeAnthropicRequest(body)
	require.NoError(t, err)
	require.Empty(t, result.Model)
	require.Len(t, result.Messages, 1)
}

func TestDecodeAnthropicRequest_InvalidJSON(t *testing.T) {
	_, err := decodeAnthropicRequest([]byte(`{invalid json`))
	require.Error(t, err)
}

// --- Tests for decodeAnthropicResponse ---

func TestDecodeAnthropicResponse_SimpleTextResponse(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-20250514",
		"content": [
			{"type": "text", "text": "The weather in NYC is sunny and 72F."}
		],
		"usage": {
			"input_tokens": 85,
			"output_tokens": 14
		}
	}`)

	result, err := decodeAnthropicResponse(body)
	require.NoError(t, err)
	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "text", result.Content[0].Type)
	require.Equal(t, "The weather in NYC is sunny and 72F.", result.Content[0].Text)
	require.NotNil(t, result.Usage)
	require.Equal(t, 85, result.Usage.InputTokens)
	require.Equal(t, 14, result.Usage.OutputTokens)
}

func TestDecodeAnthropicResponse_ToolUseResponse(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"location": "NYC"}}
		]
	}`)

	result, err := decodeAnthropicResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	require.Equal(t, "tool_use", result.Content[0].Type)
	require.Equal(t, "toolu_1", result.Content[0].ID)
	require.Equal(t, "get_weather", result.Content[0].Name)
	require.JSONEq(t, `{"location": "NYC"}`, string(result.Content[0].Input))
}

func TestDecodeAnthropicResponse_MixedContent(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me check the weather."},
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"location": "NYC"}}
		],
		"usage": {
			"input_tokens": 50,
			"output_tokens": 30
		}
	}`)

	result, err := decodeAnthropicResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Content, 2)
	require.Equal(t, "text", result.Content[0].Type)
	require.Equal(t, "tool_use", result.Content[1].Type)
}

func TestDecodeAnthropicResponse_NoUsage(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello!"}
		]
	}`)

	result, err := decodeAnthropicResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	require.Nil(t, result.Usage)
}

func TestDecodeAnthropicResponse_WithCacheTokens(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello!"}
		],
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 20,
			"cache_read_input_tokens": 30
		}
	}`)

	result, err := decodeAnthropicResponse(body)
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 50, result.Usage.OutputTokens)
	require.Equal(t, 20, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 30, result.Usage.CacheReadInputTokens)
}

func TestDecodeAnthropicResponse_EmptyContent(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": []
	}`)

	result, err := decodeAnthropicResponse(body)
	require.NoError(t, err)
	require.Empty(t, result.Content)
}

func TestDecodeAnthropicResponse_InvalidJSON(t *testing.T) {
	_, err := decodeAnthropicResponse([]byte(`{invalid json`))
	require.Error(t, err)
}

// --- Tests for extractAnthropicSystem ---

func TestExtractAnthropicSystem_String(t *testing.T) {
	require.Equal(t, "Be helpful.", extractAnthropicSystem([]byte(`"Be helpful."`)))
}

func TestExtractAnthropicSystem_Array(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"First"},{"type":"text","text":"Second"}]`)
	require.Equal(t, "First\nSecond", extractAnthropicSystem(raw))
}

func TestExtractAnthropicSystem_Empty(t *testing.T) {
	require.Empty(t, extractAnthropicSystem(nil))
	require.Empty(t, extractAnthropicSystem([]byte{}))
}

func TestExtractAnthropicSystem_Null(t *testing.T) {
	require.Empty(t, extractAnthropicSystem([]byte(`null`)))
}

// --- Tests for extractAnthropicContent ---

func TestExtractAnthropicContent_PlainString(t *testing.T) {
	require.Equal(t, "Hello, world!", extractAnthropicContent([]byte(`"Hello, world!"`)))
}

func TestExtractAnthropicContent_TextArray(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]`)
	require.Equal(t, "part1\npart2", extractAnthropicContent(raw))
}

func TestExtractAnthropicContent_MixedArray(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"hello"},{"type":"image","source":{"type":"base64","data":"..."}}]`)
	require.Equal(t, "hello", extractAnthropicContent(raw))
}

func TestExtractAnthropicContent_EmptyRaw(t *testing.T) {
	require.Empty(t, extractAnthropicContent(nil))
	require.Empty(t, extractAnthropicContent([]byte{}))
}

func TestExtractAnthropicContent_NullJSON(t *testing.T) {
	require.Empty(t, extractAnthropicContent([]byte(`null`)))
}

// --- Tests for extractAnthropicToolCalls ---

func TestExtractAnthropicToolCalls_WithToolUse(t *testing.T) {
	raw := []byte(`[
		{"type": "text", "text": "Let me check."},
		{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"location": "NYC"}}
	]`)
	toolCalls := extractAnthropicToolCalls(raw)
	require.Len(t, toolCalls, 1)
	require.Equal(t, "toolu_1", toolCalls[0].ID)
	require.Equal(t, "get_weather", toolCalls[0].Name)
}

func TestExtractAnthropicToolCalls_NoToolUse(t *testing.T) {
	raw := []byte(`[{"type": "text", "text": "Hello"}]`)
	require.Empty(t, extractAnthropicToolCalls(raw))
}

func TestExtractAnthropicToolCalls_StringContent(t *testing.T) {
	require.Empty(t, extractAnthropicToolCalls([]byte(`"Hello"`)))
}

func TestExtractAnthropicToolCalls_Empty(t *testing.T) {
	require.Empty(t, extractAnthropicToolCalls(nil))
}

// --- Tests for anthropicSSEAccumulator ---

// feedByteByByte feeds data to the accumulator one byte at a time to exercise
// incremental buffering of partial lines across multiple feed calls.
func feedByteByByte(acc *anthropicSSEAccumulator, data []byte) {
	for _, b := range data {
		acc.feed([]byte{b})
	}
}

func TestAnthropicSSEAccumulator_SimpleTextStream_ByteByByte(t *testing.T) {
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-20250514\",\"content\":[],\"usage\":{\"input_tokens\":25,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	feedByteByByte(acc, body)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "text", result.Content[0].Type)
	require.Equal(t, "Hello world", result.Content[0].Text)
	require.NotNil(t, result.Usage)
	require.Equal(t, 25, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
}

func TestAnthropicSSEAccumulator_SimpleTextStream_SingleFeed(t *testing.T) {
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-20250514\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello world\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	acc.feed(body)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "Hello world", result.Content[0].Text)
	require.NotNil(t, result.Usage)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
}

func TestAnthropicSSEAccumulator_WithToolUse(t *testing.T) {
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":20,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\",\"input\":{}}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"loc\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"ation\\\":\\\"NYC\\\"}\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":10}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	acc.feed(body)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "tool_use", result.Content[0].Type)
	require.Equal(t, "toolu_1", result.Content[0].ID)
	require.Equal(t, "get_weather", result.Content[0].Name)
	require.JSONEq(t, `{"location":"NYC"}`, string(result.Content[0].Input))
	require.NotNil(t, result.Usage)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 10, result.Usage.OutputTokens)
}

func TestAnthropicSSEAccumulator_MixedTextAndToolUse(t *testing.T) {
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":30,\"output_tokens\":0}}}\n\n" +
			// Text block at index 0
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Let me check.\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			// Tool use block at index 1
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\",\"input\":{}}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"location\\\":\\\"NYC\\\"}\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":20}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	acc.feed(body)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 2)
	require.Equal(t, "text", result.Content[0].Type)
	require.Equal(t, "Let me check.", result.Content[0].Text)
	require.Equal(t, "tool_use", result.Content[1].Type)
	require.Equal(t, "toolu_1", result.Content[1].ID)
	require.Equal(t, "get_weather", result.Content[1].Name)
	require.JSONEq(t, `{"location":"NYC"}`, string(result.Content[1].Input))
}

func TestAnthropicSSEAccumulator_NoUsage(t *testing.T) {
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	acc.feed(body)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "Hi", result.Content[0].Text)
	require.Nil(t, result.Usage)
}

func TestAnthropicSSEAccumulator_EmptyStream(t *testing.T) {
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	acc.feed(body)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Empty(t, result.Content)
}

func TestAnthropicSSEAccumulator_SkipsInvalidChunks(t *testing.T) {
	var logMessages []string
	acc := newAnthropicSSEAccumulator(func(format string, args ...any) {
		logMessages = append(logMessages, fmt.Sprintf(format, args...))
	})
	body := []byte(
		"event: message_start\n" +
			"data: {invalid json}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)
	acc.feed(body)
	result := acc.finish()

	require.Len(t, result.Content, 1)
	require.Equal(t, "OK", result.Content[0].Text)
	require.Len(t, logMessages, 1)
	require.Contains(t, logMessages[0], "failed to parse message_start")
}

func TestAnthropicSSEAccumulator_IgnoresDataAfterMessageStop(t *testing.T) {
	acc := newAnthropicSSEAccumulator(t.Logf)
	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)
	acc.feed(body)
	// Data after message_stop should be ignored.
	acc.feed([]byte(
		"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" extra\"}}\n\n",
	))
	result := acc.finish()

	require.Len(t, result.Content, 1)
	require.Equal(t, "Hi", result.Content[0].Text)
}

func TestAnthropicSSEAccumulator_PartialLineSplitAcrossFeeds(t *testing.T) {
	acc := newAnthropicSSEAccumulator(t.Logf)
	// Split a data line right in the middle.
	part1 := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],"
	part2 := "\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n"
	rest := "event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	acc.feed([]byte(part1))
	acc.feed([]byte(part2))
	acc.feed([]byte(rest))
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "OK", result.Content[0].Text)
	require.NotNil(t, result.Usage)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
}

func TestAnthropicSSEAccumulator_MultiChunkStream(t *testing.T) {
	chunk1 := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
	)
	chunk2 := []byte(
		"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	acc := newAnthropicSSEAccumulator(t.Logf)
	acc.feed(chunk1)
	acc.feed(chunk2)
	result := acc.finish()

	require.Equal(t, "assistant", result.Role)
	require.Len(t, result.Content, 1)
	require.Equal(t, "Hello world", result.Content[0].Text)
	require.NotNil(t, result.Usage)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
}
