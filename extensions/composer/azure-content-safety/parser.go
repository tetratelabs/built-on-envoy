// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"strings"
)

// Parser abstracts format-specific request/response parsing so the filter
// can work with multiple LLM API formats (Chat Completions, Responses API,
// Anthropic Messages).
type Parser interface {
	// ParseRequest extracts the user prompt and any document/system content
	// from a request body for use with the Prompt Shield API.
	ParseRequest(body []byte) (userPrompt string, documents []string, err error)

	// ParseResponse extracts the assistant's text output from a response body
	// for use with the Text Analysis and Protected Material APIs.
	ParseResponse(body []byte) (string, error)

	// ParseRequestForTaskAdherence translates a request body into the Azure
	// Task Adherence API format including tools, tool calls, and messages.
	ParseRequestForTaskAdherence(body []byte) (*taskAdherenceRequest, error)
}

// apiFormat identifies the LLM API format of a request or response body.
type apiFormat int

const (
	formatUnknown apiFormat = iota
	formatChatCompletions
	formatResponses
	formatAnthropic
)

// String returns a human-readable name for the API format.
func (f apiFormat) String() string {
	switch f {
	case formatChatCompletions:
		return "OpenAI Chat Completions"
	case formatResponses:
		return "OpenAI Responses"
	case formatAnthropic:
		return "Anthropic Messages"
	default:
		return "unknown"
	}
}

// requestProbe is used for lightweight partial JSON unmarshal to detect the
// request format without fully parsing the body.
type requestProbe struct {
	Input    json.RawMessage `json:"input"`
	Messages json.RawMessage `json:"messages"`
	System   json.RawMessage `json:"system"`
}

// detectRequestFormat probes the request body to determine the API format.
//
// Detection rules:
//  1. "input" present  -> Responses API
//  2. "messages" + top-level "system" present -> Anthropic
//  3. "messages" present -> Chat Completions (safe default; both Chat Completions
//     and Anthropic use messages with role/content, but Anthropic always has a
//     top-level "system" field)
//  4. Otherwise -> unknown
func detectRequestFormat(body []byte) apiFormat {
	var probe requestProbe
	if err := json.Unmarshal(body, &probe); err != nil {
		return formatUnknown
	}

	if len(probe.Input) > 0 {
		return formatResponses
	}
	if len(probe.Messages) > 0 {
		if len(probe.System) > 0 {
			return formatAnthropic
		}
		return formatChatCompletions
	}
	return formatUnknown
}

// responseProbe is used for lightweight partial JSON unmarshal to detect the
// response format.
type responseProbe struct {
	Output  json.RawMessage `json:"output"`
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
	Role    string          `json:"role"`
	Choices json.RawMessage `json:"choices"`
}

// detectResponseFormat probes the response body to determine the API format.
//
// Detection rules:
//  1. "output" present -> Responses API
//  2. "type"=="message" + "content" array + "role" present -> Anthropic
//  3. "choices" present -> Chat Completions
//  4. Otherwise -> unknown
func detectResponseFormat(body []byte) apiFormat {
	var probe responseProbe
	if err := json.Unmarshal(body, &probe); err != nil {
		return formatUnknown
	}

	if len(probe.Output) > 0 {
		return formatResponses
	}
	if probe.Type == "message" && len(probe.Content) > 0 && probe.Role != "" {
		return formatAnthropic
	}
	if len(probe.Choices) > 0 {
		return formatChatCompletions
	}
	return formatUnknown
}

// Singleton parser instances.
var (
	chatCompletionsParserInstance = &chatCompletionsParser{}
	responsesParserInstance       = &responsesParser{}
	anthropicParserInstance       = &anthropicParser{}
)

// parserForFormat returns the Parser implementation for the given format.
func parserForFormat(format apiFormat) Parser {
	switch format {
	case formatChatCompletions:
		return chatCompletionsParserInstance
	case formatResponses:
		return responsesParserInstance
	case formatAnthropic:
		return anthropicParserInstance
	default:
		return nil
	}
}

// titleCase converts a lowercase role name to Title Case (e.g. "user" -> "User").
func titleCase(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

// roleToSource maps LLM API roles to Azure Task Adherence source values.
func roleToSource(role string) string {
	switch role {
	case "user", "system", "developer":
		return "Prompt"
	case "assistant", "tool":
		return "Completion"
	default:
		return "Prompt"
	}
}
