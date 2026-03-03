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
	require.Equal(t, "You are a helpful assistant.", result.SystemPrompt)
	require.Equal(t, "Hello, how are you?", result.UserPrompt)
	require.Equal(t, 2, result.MessageCount)
	require.False(t, result.HasTools)
	require.False(t, result.HasToolCalls)
	require.Empty(t, result.ToolNames)
}

func TestDecodeChatRequest_MultipleUserMessages(t *testing.T) {
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
	require.Equal(t, "First question\nSecond question", result.UserPrompt)
	require.Empty(t, result.SystemPrompt)
	require.Equal(t, 3, result.MessageCount)
}

func TestDecodeChatRequest_MultipleSystemMessages(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "First system prompt"},
			{"role": "system", "content": "Second system prompt"},
			{"role": "user", "content": "Hello"}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First system prompt\nSecond system prompt", result.SystemPrompt)
	require.Equal(t, "Hello", result.UserPrompt)
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
	require.Equal(t, "What is in this image?", result.UserPrompt)
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
	require.Equal(t, "First part\nSecond part", result.UserPrompt)
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
	require.True(t, result.HasTools)
	require.Equal(t, []string{"get_weather", "send_email"}, result.ToolNames)
	require.False(t, result.HasToolCalls)
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
	require.True(t, result.HasToolCalls)
	require.Equal(t, 2, result.MessageCount)
}

func TestDecodeChatRequest_EmptyMessages(t *testing.T) {
	body := []byte(`{"model": "gpt-4o", "messages": []}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4o", result.Model)
	require.Empty(t, result.SystemPrompt)
	require.Empty(t, result.UserPrompt)
	require.Equal(t, 0, result.MessageCount)
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
	require.Equal(t, "Hello", result.UserPrompt)
}

func TestDecodeChatRequest_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json`)

	_, err := decodeChatRequest(body)
	require.Error(t, err)
}

func TestDecodeChatRequest_EmptyUserContent(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": ""}
		]
	}`)

	result, err := decodeChatRequest(body)
	require.NoError(t, err)
	require.Empty(t, result.UserPrompt)
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
