// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseChatRequest_SimpleMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello, how are you?"}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Hello, how are you?", userPrompt)
	require.Equal(t, []string{"You are a helpful assistant."}, documents)
}

func TestParseChatRequest_MultipleUserMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First question\nSecond question", userPrompt)
	require.Empty(t, documents)
}

func TestParseChatRequest_MultimodalContent(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What is in this image?"},
				{"type": "image_url", "image_url": {"url": "https://example.com/image.jpg"}}
			]}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "What is in this image?", userPrompt)
	require.Empty(t, documents)
}

func TestParseChatRequest_MultipleTextParts(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "First part"},
				{"type": "text", "text": "Second part"}
			]}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First part\nSecond part", userPrompt)
	require.Empty(t, documents)
}

func TestParseChatRequest_EmptyMessages(t *testing.T) {
	body := []byte(`{"messages": []}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Empty(t, userPrompt)
	require.Empty(t, documents)
}

func TestParseChatRequest_NoUserMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "System prompt"},
			{"role": "assistant", "content": "Hello!"}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Empty(t, userPrompt)
	require.Equal(t, []string{"System prompt"}, documents)
}

func TestParseChatRequest_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json`)

	_, _, err := ParseChatRequest(body)
	require.Error(t, err)
}

func TestParseChatRequest_EmptyContent(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": ""}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Empty(t, userPrompt)
	require.Empty(t, documents)
}

func TestParseChatRequest_MultipleSystemMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "First system prompt"},
			{"role": "system", "content": "Second system prompt"},
			{"role": "user", "content": "Hello"}
		]
	}`)

	userPrompt, documents, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Hello", userPrompt)
	require.Equal(t, []string{"First system prompt", "Second system prompt"}, documents)
}

func TestParseChatResponse_Simple(t *testing.T) {
	body := []byte(`{
		"choices": [
			{"message": {"role": "assistant", "content": "Hello! How can I help you?"}}
		]
	}`)

	content, err := ParseChatResponse(body)
	require.NoError(t, err)
	require.Equal(t, "Hello! How can I help you?", content)
}

func TestParseChatResponse_MultipleChoices(t *testing.T) {
	body := []byte(`{
		"choices": [
			{"message": {"role": "assistant", "content": "Response 1"}},
			{"message": {"role": "assistant", "content": "Response 2"}}
		]
	}`)

	content, err := ParseChatResponse(body)
	require.NoError(t, err)
	require.Equal(t, "Response 1\nResponse 2", content)
}

func TestParseChatResponse_EmptyChoices(t *testing.T) {
	body := []byte(`{"choices": []}`)

	content, err := ParseChatResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestParseChatResponse_NullMessage(t *testing.T) {
	body := []byte(`{
		"choices": [
			{"message": null}
		]
	}`)

	content, err := ParseChatResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestParseChatResponse_EmptyContent(t *testing.T) {
	body := []byte(`{
		"choices": [
			{"message": {"role": "assistant", "content": ""}}
		]
	}`)

	content, err := ParseChatResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestParseChatResponse_InvalidJSON(t *testing.T) {
	_, err := ParseChatResponse([]byte(`{invalid`))
	require.Error(t, err)
}

func TestParseChatResponse_StreamingSSE(t *testing.T) {
	// SSE format should fail to parse as a standard JSON response.
	sseBody := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: [DONE]\n\n")

	_, err := ParseChatResponse(sseBody)
	require.Error(t, err)
}

// Tests for parseChatRequestForTaskAdherence

func TestParseChatRequestForTaskAdherence_SimpleMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": "Let me check that for you."}
		]
	}`)

	result, err := parseChatRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 3)

	// System message.
	require.Equal(t, "System", result.Messages[0].Role)
	require.Equal(t, "Prompt", result.Messages[0].Source)
	require.Equal(t, "You are a helpful assistant.", result.Messages[0].Contents)

	// User message.
	require.Equal(t, "User", result.Messages[1].Role)
	require.Equal(t, "Prompt", result.Messages[1].Source)
	require.Equal(t, "What is the weather?", result.Messages[1].Contents)

	// Assistant message.
	require.Equal(t, "Assistant", result.Messages[2].Role)
	require.Equal(t, "Completion", result.Messages[2].Source)
	require.Equal(t, "Let me check that for you.", result.Messages[2].Contents)
}

func TestParseChatRequestForTaskAdherence_WithTools(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "What is the weather?"}
		],
		"tools": [
			{"type": "function", "function": {"name": "get_weather", "description": "Get weather info"}},
			{"type": "function", "function": {"name": "delete_all", "description": "Delete all data"}}
		]
	}`)

	result, err := parseChatRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)
	require.Equal(t, "function", result.Tools[0].Type)
	require.Equal(t, "get_weather", result.Tools[0].Function.Name)
	require.Equal(t, "Get weather info", result.Tools[0].Function.Description)
	require.Equal(t, "delete_all", result.Tools[1].Function.Name)
}

func TestParseChatRequestForTaskAdherence_WithToolCalls(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "delete_all", "arguments": "{}"}}
			]}
		]
	}`)

	result, err := parseChatRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 2)

	assistantMsg := result.Messages[1]
	require.Equal(t, "Assistant", assistantMsg.Role)
	require.Equal(t, "Completion", assistantMsg.Source)
	require.Len(t, assistantMsg.ToolCalls, 1)
	require.Equal(t, "call_1", assistantMsg.ToolCalls[0].ID)
	require.Equal(t, "function", assistantMsg.ToolCalls[0].Type)
	require.Equal(t, "delete_all", assistantMsg.ToolCalls[0].Function.Name)
	require.Equal(t, "{}", assistantMsg.ToolCalls[0].Function.Arguments)
}

func TestParseChatRequestForTaskAdherence_ToolRoleMessage(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "tool", "content": "Weather is sunny", "tool_call_id": "call_1"}
		]
	}`)

	result, err := parseChatRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)

	toolMsg := result.Messages[0]
	require.Equal(t, "Tool", toolMsg.Role)
	require.Equal(t, "Completion", toolMsg.Source)
	require.Equal(t, "Weather is sunny", toolMsg.Contents)
	require.Equal(t, "call_1", toolMsg.ToolCallID)
}

func TestParseChatRequestForTaskAdherence_EmptyMessages(t *testing.T) {
	body := []byte(`{"messages": []}`)

	result, err := parseChatRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Empty(t, result.Messages)
	require.Empty(t, result.Tools)
}

func TestParseChatRequestForTaskAdherence_InvalidJSON(t *testing.T) {
	_, err := parseChatRequestForTaskAdherence([]byte(`{invalid`))
	require.Error(t, err)
}
