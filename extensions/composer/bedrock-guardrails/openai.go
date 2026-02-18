// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// contentPart represents a single part in a multimodal content array.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ParseChatRequest parses an OpenAI chat completions request body and extracts
// user prompts and system/document messages for use with Prompt Shield.
func ParseChatRequest(body []byte) (userParts []string, err error) {
	var req CreateChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	for _, msg := range req.Messages {
		if msg.Role == "user" {
			text := extractContent(msg.Content)
			userParts = append(userParts, text)
		}
	}

	return userParts, nil
}

// ReplaceUserPrompts replaces user messages in the CreateChatCompletionRequest
// represented in the provided bodyBytes with the provided list of messages
func ReplaceUserPrompts(requestBytes []byte, messages []string) ([]byte, error) {
	var req CreateChatCompletionRequest
	if err := json.Unmarshal(requestBytes, &req); err != nil {
		return nil, err
	}

	userMessageIndex := 0
	var m []ChatCompletionRequestMessage
	for _, msg := range req.Messages {
		if userMessageIndex == len(messages) {
			return nil, fmt.Errorf("there are more messages thand found in the request")
		}
		// We're only dealing with user messages
		if msg.Role != "user" {
			continue
		}

		var rm json.RawMessage
		if isString(msg.Content) {
			rm = []byte(fmt.Sprintf("%q", messages[userMessageIndex])) //nolint:gosec
		}
		if isText(msg.Content) {
			c := contentPart{
				Type: "text",
				Text: messages[userMessageIndex], //nolint:gosec
			}
			b, err := json.Marshal(c)
			if err != nil {
				return nil, fmt.Errorf("marshal content: %w", err)
			}
			rm = b
		}
		msg.Content = rm
		m = append(m, msg)
		userMessageIndex++
	}
	req.Messages = m

	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal req with replaced texts: %w", err)
	}

	return b, nil
}

// isText returns true if the privded Content is of type "text" or false otherwise
func isText(content json.RawMessage) bool {
	if len(content) == 0 {
		return false
	}

	var parts []contentPart
	if err := json.Unmarshal(content, &parts); err == nil {
		return true
	}

	return false
}

// isString returns true if the privded Content is of type "text" or false otherwise
func isString(content json.RawMessage) bool {
	if len(content) == 0 {
		return false
	}

	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return true
	}

	return false
}

// extractContent handles both string and array content formats.
func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content parts.
	var parts []contentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return joinStrings(texts, "\n")
	}

	return ""
}

func joinStrings(parts []string, sep string) string {
	return strings.Join(parts, sep)
}
