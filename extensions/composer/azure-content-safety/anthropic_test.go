// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for anthropicParser.ParseRequest

func TestAnthropicParseRequest_StringSystem(t *testing.T) {
	body := []byte(`{
		"system": "You are a helpful assistant.",
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`)

	p := &anthropicParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Hello!", userPrompt)
	require.Equal(t, []string{"You are a helpful assistant."}, documents)
}

func TestAnthropicParseRequest_ArraySystem(t *testing.T) {
	body := []byte(`{
		"system": [
			{"type": "text", "text": "First system instruction"},
			{"type": "text", "text": "Second system instruction"}
		],
		"messages": [
			{"role": "user", "content": "Hi"}
		]
	}`)

	p := &anthropicParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Hi", userPrompt)
	require.Equal(t, []string{"First system instruction\nSecond system instruction"}, documents)
}

func TestAnthropicParseRequest_ContentBlockArray(t *testing.T) {
	body := []byte(`{
		"system": "Be helpful.",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "First part"},
				{"type": "image", "source": {"type": "base64", "data": "..."}},
				{"type": "text", "text": "Second part"}
			]}
		]
	}`)

	p := &anthropicParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First part\nSecond part", userPrompt)
	require.Equal(t, []string{"Be helpful."}, documents)
}

func TestAnthropicParseRequest_MultipleUserMessages(t *testing.T) {
	body := []byte(`{
		"system": "Be helpful.",
		"messages": [
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		]
	}`)

	p := &anthropicParser{}
	userPrompt, _, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First question\nSecond question", userPrompt)
}

func TestAnthropicParseRequest_NoSystemField(t *testing.T) {
	body := []byte(`{
		"system": "sys",
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)

	p := &anthropicParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Hello", userPrompt)
	require.Equal(t, []string{"sys"}, documents)
}

func TestAnthropicParseRequest_EmptyMessages(t *testing.T) {
	body := []byte(`{"system": "sys", "messages": []}`)

	p := &anthropicParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Empty(t, userPrompt)
	require.Equal(t, []string{"sys"}, documents)
}

func TestAnthropicParseRequest_InvalidJSON(t *testing.T) {
	p := &anthropicParser{}
	_, _, err := p.ParseRequest([]byte(`{invalid`))
	require.Error(t, err)
}

// Tests for anthropicParser.ParseResponse

func TestAnthropicParseResponse_SingleTextBlock(t *testing.T) {
	body := []byte(`{
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello! How can I help you?"}
		]
	}`)

	p := &anthropicParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Equal(t, "Hello! How can I help you?", content)
}

func TestAnthropicParseResponse_MultipleContentBlocks(t *testing.T) {
	body := []byte(`{
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Here is my analysis:"},
			{"type": "tool_use", "id": "toolu_1", "name": "calculator", "input": {"expr": "2+2"}},
			{"type": "text", "text": "The result is 4."}
		]
	}`)

	p := &anthropicParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Equal(t, "Here is my analysis:\nThe result is 4.", content)
}

func TestAnthropicParseResponse_EmptyContent(t *testing.T) {
	body := []byte(`{
		"type": "message",
		"role": "assistant",
		"content": []
	}`)

	p := &anthropicParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestAnthropicParseResponse_ToolUseOnly(t *testing.T) {
	body := []byte(`{
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"location": "Seattle"}}
		]
	}`)

	p := &anthropicParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestAnthropicParseResponse_InvalidJSON(t *testing.T) {
	p := &anthropicParser{}
	_, err := p.ParseResponse([]byte(`{invalid`))
	require.Error(t, err)
}

// Tests for anthropicParser.ParseRequestForTaskAdherence

func TestAnthropicParseRequestForTaskAdherence_SimpleMessages(t *testing.T) {
	body := []byte(`{
		"system": "You are a helpful assistant.",
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": "Let me check."}
		]
	}`)

	p := &anthropicParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)

	// System + user + assistant = 3 messages.
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
	require.Equal(t, "Let me check.", result.Messages[2].Contents)
}

func TestAnthropicParseRequestForTaskAdherence_WithTools(t *testing.T) {
	body := []byte(`{
		"system": "Be helpful.",
		"messages": [
			{"role": "user", "content": "What is the weather?"}
		],
		"tools": [
			{"name": "get_weather", "description": "Get weather info", "input_schema": {"type": "object"}},
			{"name": "delete_all", "description": "Delete all data", "input_schema": {"type": "object"}}
		]
	}`)

	p := &anthropicParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)
	require.Equal(t, "function", result.Tools[0].Type)
	require.Equal(t, "get_weather", result.Tools[0].Function.Name)
	require.Equal(t, "Get weather info", result.Tools[0].Function.Description)
	require.Equal(t, "delete_all", result.Tools[1].Function.Name)
}

func TestAnthropicParseRequestForTaskAdherence_WithToolUse(t *testing.T) {
	body := []byte(`{
		"system": "Be helpful.",
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Let me check."},
				{"type": "tool_use", "id": "toolu_1", "name": "delete_all", "input": {"confirm": true}}
			]}
		]
	}`)

	p := &anthropicParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)

	// System + user + assistant = 3 messages.
	require.Len(t, result.Messages, 3)

	// Assistant message should have tool call.
	assistantMsg := result.Messages[2]
	require.Equal(t, "Assistant", assistantMsg.Role)
	require.Equal(t, "Completion", assistantMsg.Source)
	require.Equal(t, "Let me check.", assistantMsg.Contents)
	require.Len(t, assistantMsg.ToolCalls, 1)
	require.Equal(t, "toolu_1", assistantMsg.ToolCalls[0].ID)
	require.Equal(t, "function", assistantMsg.ToolCalls[0].Type)
	require.Equal(t, "delete_all", assistantMsg.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"confirm":true}`, assistantMsg.ToolCalls[0].Function.Arguments)
}

func TestAnthropicParseRequestForTaskAdherence_WithToolResult(t *testing.T) {
	body := []byte(`{
		"system": "Be helpful.",
		"messages": [
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "Weather is sunny"}
			]}
		]
	}`)

	p := &anthropicParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)

	// System + user message + tool result message = 3 messages.
	require.Len(t, result.Messages, 3)

	// Tool result extracted from user message.
	toolMsg := result.Messages[2]
	require.Equal(t, "Tool", toolMsg.Role)
	require.Equal(t, "Completion", toolMsg.Source)
	require.Equal(t, "Weather is sunny", toolMsg.Contents)
	require.Equal(t, "toolu_1", toolMsg.ToolCallID)
}

func TestAnthropicParseRequestForTaskAdherence_InvalidJSON(t *testing.T) {
	p := &anthropicParser{}
	_, err := p.ParseRequestForTaskAdherence([]byte(`{invalid`))
	require.Error(t, err)
}
