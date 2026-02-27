// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for ParseChatRequest

func TestParseChatRequest_SimpleUserMessage(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "Hello, world!"}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{"Hello, world!"}, userPrompt)
}

func TestParseChatRequest_MultipleUserMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "First message"},
			{"role": "user", "content": "Second message"}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{"First message", "Second message"}, userPrompt)
}

func TestParseChatRequest_ArrayContent(t *testing.T) {
	body := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "First part"},
					{"type": "text", "text": "Second part"}
				]
			}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{"First part\nSecond part"}, userPrompt)
}

func TestParseChatRequest_ArrayContentWithNonText(t *testing.T) {
	body := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Text part"},
					{"type": "image_url", "image_url": {"url": "https://example.com/image.jpg"}}
				]
			}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{"Text part"}, userPrompt)
}

func TestParseChatRequest_MixedRoles(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "System prompt"},
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{"First question", "Second question"}, userPrompt)
}

func TestParseChatRequest_EmptyContent(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": ""}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{""}, userPrompt)
}

func TestParseChatRequest_NoMessages(t *testing.T) {
	body := []byte(`{"messages": []}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Empty(t, userPrompt)
}

func TestParseChatRequest_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json}`)

	userPrompt, err := ParseChatRequest(body)
	require.Error(t, err)
	require.Empty(t, userPrompt)
}

func TestParseChatRequest_EmptyBody(t *testing.T) {
	body := []byte(``)

	userPrompt, err := ParseChatRequest(body)
	require.Error(t, err)
	require.Empty(t, userPrompt)
}

func TestParseChatRequest_EmptyArrayContent(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": []}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{""}, userPrompt)
}

func TestParseChatRequest_ArrayContentEmptyText(t *testing.T) {
	body := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": ""}
				]
			}
		]
	}`)

	userPrompt, err := ParseChatRequest(body)
	require.NoError(t, err)
	require.Equal(t, []string{""}, userPrompt)
}

// Tests for extractContent

func TestExtractContent_EmptyRawMessage(t *testing.T) {
	result := extractContent(nil)
	require.Empty(t, result)
}

func TestExtractContent_StringContent(t *testing.T) {
	raw := []byte(`"Simple string content"`)
	result := extractContent(raw)
	require.Equal(t, "Simple string content", result)
}

func TestExtractContent_ArrayContent(t *testing.T) {
	raw := []byte(`[
		{"type": "text", "text": "Part 1"},
		{"type": "text", "text": "Part 2"}
	]`)
	result := extractContent(raw)
	require.Equal(t, "Part 1\nPart 2", result)
}

func TestExtractContent_InvalidJSON(t *testing.T) {
	raw := []byte(`{invalid}`)
	result := extractContent(raw)
	require.Empty(t, result)
}

// Tests for joinStrings

func TestJoinStrings_MultipleParts(t *testing.T) {
	parts := []string{"a", "b", "c"}
	result := joinStrings(parts, ",")
	require.Equal(t, "a,b,c", result)
}

func TestJoinStrings_EmptySlice(t *testing.T) {
	parts := []string{}
	result := joinStrings(parts, ",")
	require.Empty(t, result)
}

func TestJoinStrings_SinglePart(t *testing.T) {
	parts := []string{"only"}
	result := joinStrings(parts, ",")
	require.Equal(t, "only", result)
}

func TestJoinStrings_EmptyStrings(t *testing.T) {
	parts := []string{"", "", ""}
	result := joinStrings(parts, ",")
	require.Equal(t, ",,", result)
}

// Tests for ReplaceUserPrompts

func TestReplaceUserPrompts_ArrayContent_SingleUserMessage(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Original text"}]}
		],
		"model": "gpt-4o"
	}`)

	result, err := ReplaceUserPrompts(body, []string{"Replacement text"})
	require.NoError(t, err)

	var req CreateChatCompletionRequest
	require.NoError(t, json.Unmarshal(result, &req))
	require.Len(t, req.Messages, 1)
	require.Equal(t, "user", req.Messages[0].Role)

	var part contentPart
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &part))
	require.Equal(t, "text", part.Type)
	require.Equal(t, "Replacement text", part.Text)
}

func TestReplaceUserPrompts_ArrayContent_MultipleUserMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "First original"}]},
			{"role": "user", "content": [{"type": "text", "text": "Second original"}]}
		],
		"model": "gpt-4o"
	}`)

	result, err := ReplaceUserPrompts(body, []string{"First replacement", "Second replacement"})
	require.NoError(t, err)

	var req CreateChatCompletionRequest
	require.NoError(t, json.Unmarshal(result, &req))
	require.Len(t, req.Messages, 2)

	var part0 contentPart
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &part0))
	require.Equal(t, "First replacement", part0.Text)

	var part1 contentPart
	require.NoError(t, json.Unmarshal(req.Messages[1].Content, &part1))
	require.Equal(t, "Second replacement", part1.Text)
}

func TestReplaceUserPrompts_PreservesNonUserMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": [{"type": "text", "text": "Original"}]},
			{"role": "assistant", "content": "Sure!"},
			{"role": "user", "content": [{"type": "text", "text": "Follow-up"}]}
		],
		"model": "gpt-4o"
	}`)

	result, err := ReplaceUserPrompts(body, []string{"Replacement 1", "Replacement 2"})
	require.NoError(t, err)

	var req CreateChatCompletionRequest
	require.NoError(t, json.Unmarshal(result, &req))
	// The implementation only retains user messages in the output.
	require.Len(t, req.Messages, 2)

	var part0 contentPart
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &part0))
	require.Equal(t, "user", req.Messages[0].Role)
	require.Equal(t, "Replacement 1", part0.Text)

	var part1 contentPart
	require.NoError(t, json.Unmarshal(req.Messages[1].Content, &part1))
	require.Equal(t, "user", req.Messages[1].Role)
	require.Equal(t, "Replacement 2", part1.Text)
}

func TestReplaceUserPrompts_PreservesModelField(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Original"}]}
		],
		"model": "gpt-4o",
		"stream": true
	}`)

	result, err := ReplaceUserPrompts(body, []string{"Replacement"})
	require.NoError(t, err)

	var req CreateChatCompletionRequest
	require.NoError(t, json.Unmarshal(result, &req))
	require.Equal(t, "gpt-4o", req.Model)
	require.NotNil(t, req.Stream)
	require.True(t, *req.Stream)
}

func TestReplaceUserPrompts_NoUserMessages(t *testing.T) {
	// The implementation checks userMessageIndex == len(messages) before
	// filtering by role, so passing an empty replacements slice with any
	// messages in the request triggers an error immediately.
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "System message"}
		],
		"model": "gpt-4o"
	}`)

	_, err := ReplaceUserPrompts(body, []string{})
	require.Error(t, err)
}

func TestReplaceUserPrompts_StringContent_ReplacesSuccessfully(t *testing.T) {
	// The string content path uses fmt.Sprintf("%q", ...) which produces a
	// valid JSON string, so marshalling succeeds.
	body := []byte(`{
		"messages": [
			{"role": "user", "content": "Hello, world!"}
		],
		"model": "gpt-4o"
	}`)

	result, err := ReplaceUserPrompts(body, []string{"Replacement"})
	require.NoError(t, err)

	var req CreateChatCompletionRequest
	require.NoError(t, json.Unmarshal(result, &req))
	require.Len(t, req.Messages, 1)

	var content string
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &content))
	require.Equal(t, "Replacement", content)
}

func TestReplaceUserPrompts_InvalidJSON(t *testing.T) {
	_, err := ReplaceUserPrompts([]byte(`{invalid json}`), []string{"x"})
	require.Error(t, err)
}

func TestReplaceUserPrompts_EmptyBody(t *testing.T) {
	_, err := ReplaceUserPrompts([]byte(``), []string{"x"})
	require.Error(t, err)
}
