// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeChatRequest_SimpleMessages(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello, how are you?"}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4o", result.Model)
	require.Len(t, result.Messages, 2)
	require.Equal(t, "system", result.Messages[0].Role)
	require.Equal(t, "You are a helpful assistant.", extractContent(result.Messages[0].Content))
	require.Equal(t, "user", result.Messages[1].Role)
	require.Equal(t, "Hello, how are you?", extractContent(result.Messages[1].Content))
	require.Empty(t, result.Tools)
}

func TestDecodeChatRequest_MultiTurnConversation(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 3)
	require.Equal(t, "user", result.Messages[0].Role)
	require.Equal(t, "First question", extractContent(result.Messages[0].Content))
	require.Equal(t, "assistant", result.Messages[1].Role)
	require.Equal(t, "First answer", extractContent(result.Messages[1].Content))
	require.Equal(t, "user", result.Messages[2].Role)
	require.Equal(t, "Second question", extractContent(result.Messages[2].Content))
}

func TestDecodeChatRequest_MultimodalContent(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What is in this image?"},
				{"type": "image_url", "image_url": {"url": "https://example.com/image.jpg"}}
			]}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	require.Equal(t, "What is in this image?", extractContent(result.Messages[0].Content))
}

func TestDecodeChatRequest_MultipleTextParts(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "First part"},
				{"type": "text", "text": "Second part"}
			]}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	require.Equal(t, "First part\nSecond part", extractContent(result.Messages[0].Content))
}

func TestDecodeChatRequest_WithTools(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "What is the weather?"}
		],
		"tools": [
			{"type": "function", "function": {"name": "get_weather", "description": "Get weather info"}},
			{"type": "function", "function": {"name": "send_email", "description": "Send an email"}}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)
	require.Equal(t, "get_weather", result.Tools[0].Function.Name)
	require.Equal(t, "send_email", result.Tools[1].Function.Name)
}

func TestDecodeChatRequest_WithToolCalls(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\": \"NYC\"}"}}
			]}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 2)
	require.Equal(t, "assistant", result.Messages[1].Role)
	require.Len(t, result.Messages[1].ToolCalls, 1)
	require.Equal(t, "call_1", result.Messages[1].ToolCalls[0].ID)
	require.Equal(t, "get_weather", result.Messages[1].ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"location": "NYC"}`, result.Messages[1].ToolCalls[0].Function.Arguments)
}

func TestDecodeChatRequest_EmptyMessages(t *testing.T) {
	body := []byte(`{"model": "gpt-4o", "messages": []}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4o", result.Model)
	require.Empty(t, result.Messages)
	require.Empty(t, result.Tools)
}

func TestDecodeChatRequest_NoModel(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Empty(t, result.Model)
	require.Len(t, result.Messages, 1)
}

func TestDecodeChatRequest_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json`)

	_, err := decodeChatRequest(body)
	require.Error(t, err)
}

func TestDecodeChatResponse_SimpleResponse(t *testing.T) {
	body := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4o",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "The weather in NYC is sunny and 72F."
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 85,
			"completion_tokens": 14,
			"total_tokens": 99
		}
	}`)

	result, err := decodeChatResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Choices, 1)
	require.Equal(t, "assistant", result.Choices[0].Message.Role)
	require.Equal(t, "The weather in NYC is sunny and 72F.", extractContent(result.Choices[0].Message.Content))
	require.NotNil(t, result.Usage)
	require.Equal(t, 85, result.Usage.PromptTokens)
	require.Equal(t, 14, result.Usage.CompletionTokens)
}

func TestDecodeChatResponse_NoUsage(t *testing.T) {
	body := []byte(`{
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello!"
				},
				"finish_reason": "stop"
			}
		]
	}`)

	result, err := decodeChatResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Choices, 1)
	require.Nil(t, result.Usage)
}

func TestDecodeChatResponse_MultipleChoices(t *testing.T) {
	body := []byte(`{
		"choices": [
			{"index": 0, "message": {"role": "assistant", "content": "Answer 1"}},
			{"index": 1, "message": {"role": "assistant", "content": "Answer 2"}}
		]
	}`)

	result, err := decodeChatResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Choices, 2)
	require.Equal(t, "Answer 1", extractContent(result.Choices[0].Message.Content))
	require.Equal(t, "Answer 2", extractContent(result.Choices[1].Message.Content))
}

func TestDecodeChatResponse_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json`)

	_, err := decodeChatResponse(body)
	require.Error(t, err)
}

func TestDecodeChatResponse_EmptyChoices(t *testing.T) {
	body := []byte(`{"choices": []}`)

	result, err := decodeChatResponse(body)
	require.NoError(t, err)
	require.Empty(t, result.Choices)
	require.Nil(t, result.Usage)
}

func TestExtractContent_PlainString(t *testing.T) {
	raw := []byte(`"Hello, world!"`)
	require.Equal(t, "Hello, world!", extractContent(raw))
}

func TestExtractContent_TextArray(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]`)
	require.Equal(t, "part1\npart2", extractContent(raw))
}

func TestExtractContent_MixedArray(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.com"}}]`)
	require.Equal(t, "hello", extractContent(raw))
}

func TestExtractContent_EmptyRaw(t *testing.T) {
	require.Empty(t, extractContent(nil))
	require.Empty(t, extractContent([]byte{}))
}

func TestExtractContent_NullJSON(t *testing.T) {
	require.Empty(t, extractContent([]byte(`null`)))
}
