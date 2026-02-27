// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for responsesParser.ParseRequest

func TestResponsesParseRequest_SimpleStringInput(t *testing.T) {
	body := []byte(`{"input": "Tell me a story"}`)

	p := &responsesParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Tell me a story", userPrompt)
	require.Empty(t, documents)
}

func TestResponsesParseRequest_ArrayInput_UserMessage(t *testing.T) {
	body := []byte(`{
		"input": [
			{"role": "user", "content": "Hello, how are you?"}
		]
	}`)

	p := &responsesParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "Hello, how are you?", userPrompt)
	require.Empty(t, documents)
}

func TestResponsesParseRequest_ArrayInput_DeveloperAndUser(t *testing.T) {
	body := []byte(`{
		"input": [
			{"role": "developer", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the weather?"}
		]
	}`)

	p := &responsesParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "What is the weather?", userPrompt)
	require.Equal(t, []string{"You are a helpful assistant."}, documents)
}

func TestResponsesParseRequest_ArrayInput_ContentArray(t *testing.T) {
	body := []byte(`{
		"input": [
			{"role": "user", "content": [
				{"type": "input_text", "text": "First part"},
				{"type": "input_text", "text": "Second part"}
			]}
		]
	}`)

	p := &responsesParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First part\nSecond part", userPrompt)
	require.Empty(t, documents)
}

func TestResponsesParseRequest_MultipleUserMessages(t *testing.T) {
	body := []byte(`{
		"input": [
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		]
	}`)

	p := &responsesParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Equal(t, "First question\nSecond question", userPrompt)
	require.Empty(t, documents)
}

func TestResponsesParseRequest_EmptyInput(t *testing.T) {
	body := []byte(`{"input": []}`)

	p := &responsesParser{}
	userPrompt, documents, err := p.ParseRequest(body)
	require.NoError(t, err)
	require.Empty(t, userPrompt)
	require.Empty(t, documents)
}

func TestResponsesParseRequest_InvalidJSON(t *testing.T) {
	p := &responsesParser{}
	_, _, err := p.ParseRequest([]byte(`{invalid`))
	require.Error(t, err)
}

// Tests for responsesParser.ParseResponse

func TestResponsesParseResponse_SingleTextOutput(t *testing.T) {
	body := []byte(`{
		"output": [
			{"type": "message", "content": [{"type": "output_text", "text": "Hello! How can I help?"}]}
		]
	}`)

	p := &responsesParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Equal(t, "Hello! How can I help?", content)
}

func TestResponsesParseResponse_MultipleOutputItems(t *testing.T) {
	body := []byte(`{
		"output": [
			{"type": "message", "content": [{"type": "output_text", "text": "Part 1"}]},
			{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{}"},
			{"type": "message", "content": [{"type": "output_text", "text": "Part 2"}]}
		]
	}`)

	p := &responsesParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Equal(t, "Part 1\nPart 2", content)
}

func TestResponsesParseResponse_EmptyOutput(t *testing.T) {
	body := []byte(`{"output": []}`)

	p := &responsesParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestResponsesParseResponse_FunctionCallOnly(t *testing.T) {
	body := []byte(`{
		"output": [
			{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{}"}
		]
	}`)

	p := &responsesParser{}
	content, err := p.ParseResponse(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestResponsesParseResponse_InvalidJSON(t *testing.T) {
	p := &responsesParser{}
	_, err := p.ParseResponse([]byte(`{invalid`))
	require.Error(t, err)
}

// Tests for responsesParser.ParseRequestForTaskAdherence

func TestResponsesParseRequestForTaskAdherence_SimpleInput(t *testing.T) {
	body := []byte(`{"input": "What is the weather?"}`)

	p := &responsesParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	require.Equal(t, "User", result.Messages[0].Role)
	require.Equal(t, "Prompt", result.Messages[0].Source)
	require.Equal(t, "What is the weather?", result.Messages[0].Contents)
}

func TestResponsesParseRequestForTaskAdherence_WithTools(t *testing.T) {
	body := []byte(`{
		"input": [{"role": "user", "content": "What is the weather?"}],
		"tools": [
			{"type": "function", "name": "get_weather", "description": "Get weather info"},
			{"type": "web_search", "name": "web_search", "description": "Search the web"}
		]
	}`)

	p := &responsesParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)
	// Only function tools are translated.
	require.Len(t, result.Tools, 1)
	require.Equal(t, "function", result.Tools[0].Type)
	require.Equal(t, "get_weather", result.Tools[0].Function.Name)
}

func TestResponsesParseRequestForTaskAdherence_WithFunctionCall(t *testing.T) {
	body := []byte(`{
		"input": [
			{"role": "user", "content": "What is the weather?"},
			{"type": "function_call", "call_id": "call_1", "name": "delete_all", "arguments": "{}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "All data deleted"}
		]
	}`)

	p := &responsesParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 3)

	// User message.
	require.Equal(t, "User", result.Messages[0].Role)
	require.Equal(t, "Prompt", result.Messages[0].Source)

	// Function call → assistant tool call.
	require.Equal(t, "Assistant", result.Messages[1].Role)
	require.Equal(t, "Completion", result.Messages[1].Source)
	require.Len(t, result.Messages[1].ToolCalls, 1)
	require.Equal(t, "call_1", result.Messages[1].ToolCalls[0].ID)
	require.Equal(t, "delete_all", result.Messages[1].ToolCalls[0].Function.Name)
	require.Equal(t, "{}", result.Messages[1].ToolCalls[0].Function.Arguments)

	// Function call output → tool message.
	require.Equal(t, "Tool", result.Messages[2].Role)
	require.Equal(t, "Completion", result.Messages[2].Source)
	require.Equal(t, "All data deleted", result.Messages[2].Contents)
	require.Equal(t, "call_1", result.Messages[2].ToolCallID)
}

func TestResponsesParseRequestForTaskAdherence_DeveloperRole(t *testing.T) {
	body := []byte(`{
		"input": [
			{"role": "developer", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello"}
		]
	}`)

	p := &responsesParser{}
	result, err := p.ParseRequestForTaskAdherence(body)
	require.NoError(t, err)
	require.Len(t, result.Messages, 2)
	require.Equal(t, "Developer", result.Messages[0].Role)
	require.Equal(t, "Prompt", result.Messages[0].Source)
}

func TestResponsesParseRequestForTaskAdherence_InvalidJSON(t *testing.T) {
	p := &responsesParser{}
	_, err := p.ParseRequestForTaskAdherence([]byte(`{invalid`))
	require.Error(t, err)
}
