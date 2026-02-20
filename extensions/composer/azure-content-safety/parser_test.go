// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectRequestFormat_ChatCompletions(t *testing.T) {
	body := []byte(`{"messages": [{"role": "user", "content": "hello"}]}`)
	require.Equal(t, formatChatCompletions, detectRequestFormat(body))
}

func TestDetectRequestFormat_ChatCompletionsWithSystemRole(t *testing.T) {
	// Chat Completions with system role inside messages (no top-level "system" field).
	body := []byte(`{"messages": [{"role": "system", "content": "You are helpful"}, {"role": "user", "content": "hi"}]}`)
	require.Equal(t, formatChatCompletions, detectRequestFormat(body))
}

func TestDetectRequestFormat_Responses_StringInput(t *testing.T) {
	body := []byte(`{"input": "Tell me a story"}`)
	require.Equal(t, formatResponses, detectRequestFormat(body))
}

func TestDetectRequestFormat_Responses_ArrayInput(t *testing.T) {
	body := []byte(`{"input": [{"role": "user", "content": "hello"}]}`)
	require.Equal(t, formatResponses, detectRequestFormat(body))
}

func TestDetectRequestFormat_Anthropic(t *testing.T) {
	body := []byte(`{"system": "You are helpful", "messages": [{"role": "user", "content": "hi"}]}`)
	require.Equal(t, formatAnthropic, detectRequestFormat(body))
}

func TestDetectRequestFormat_Anthropic_ArraySystem(t *testing.T) {
	body := []byte(`{"system": [{"type": "text", "text": "You are helpful"}], "messages": [{"role": "user", "content": "hi"}]}`)
	require.Equal(t, formatAnthropic, detectRequestFormat(body))
}

func TestDetectRequestFormat_Unknown(t *testing.T) {
	body := []byte(`{"query": "some non-chat request"}`)
	require.Equal(t, formatUnknown, detectRequestFormat(body))
}

func TestDetectRequestFormat_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json`)
	require.Equal(t, formatUnknown, detectRequestFormat(body))
}

func TestDetectRequestFormat_EmptyObject(t *testing.T) {
	body := []byte(`{}`)
	require.Equal(t, formatUnknown, detectRequestFormat(body))
}

func TestDetectResponseFormat_ChatCompletions(t *testing.T) {
	body := []byte(`{"choices": [{"message": {"role": "assistant", "content": "hi"}}]}`)
	require.Equal(t, formatChatCompletions, detectResponseFormat(body))
}

func TestDetectResponseFormat_Responses(t *testing.T) {
	body := []byte(`{"output": [{"type": "message", "content": [{"type": "output_text", "text": "hi"}]}]}`)
	require.Equal(t, formatResponses, detectResponseFormat(body))
}

func TestDetectResponseFormat_Anthropic(t *testing.T) {
	body := []byte(`{"type": "message", "role": "assistant", "content": [{"type": "text", "text": "hi"}]}`)
	require.Equal(t, formatAnthropic, detectResponseFormat(body))
}

func TestDetectResponseFormat_Unknown(t *testing.T) {
	body := []byte(`{"result": "some value"}`)
	require.Equal(t, formatUnknown, detectResponseFormat(body))
}

func TestDetectResponseFormat_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	require.Equal(t, formatUnknown, detectResponseFormat(body))
}

func TestParserForFormat(t *testing.T) {
	require.IsType(t, &chatCompletionsParser{}, parserForFormat(formatChatCompletions))
	require.IsType(t, &responsesParser{}, parserForFormat(formatResponses))
	require.IsType(t, &anthropicParser{}, parserForFormat(formatAnthropic))
	require.Nil(t, parserForFormat(formatUnknown))
}

func TestAPIFormatString(t *testing.T) {
	require.Equal(t, "OpenAI Chat Completions", formatChatCompletions.String())
	require.Equal(t, "OpenAI Responses", formatResponses.String())
	require.Equal(t, "Anthropic Messages", formatAnthropic.String())
	require.Equal(t, "unknown", formatUnknown.String())
}

func TestTitleCase(t *testing.T) {
	require.Equal(t, "User", titleCase("user"))
	require.Equal(t, "Assistant", titleCase("assistant"))
	require.Equal(t, "System", titleCase("system"))
	require.Equal(t, "Developer", titleCase("developer"))
	require.Empty(t, titleCase(""))
}

func TestRoleToSource(t *testing.T) {
	require.Equal(t, "Prompt", roleToSource("user"))
	require.Equal(t, "Prompt", roleToSource("system"))
	require.Equal(t, "Prompt", roleToSource("developer"))
	require.Equal(t, "Completion", roleToSource("assistant"))
	require.Equal(t, "Completion", roleToSource("tool"))
	require.Equal(t, "Prompt", roleToSource("unknown"))
}
