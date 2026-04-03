// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	llm "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/llm"
)

// Anthropic content block type constants.
const (
	blockTypeText                = "text"
	blockTypeImage               = "image"
	blockTypeDocument            = "document"
	blockTypeSearchResult        = "search_result"
	blockTypeToolUse             = "tool_use"
	blockTypeToolResult          = "tool_result"
	blockTypeThinking            = "thinking"
	blockTypeRedactedThinking    = "redacted_thinking"
	blockTypeServerToolUse       = "server_tool_use"
	blockTypeWebSearchToolResult = "web_search_tool_result"
)

// Anthropic stop reason constants.
const (
	stopReasonEndTurn      = "end_turn"
	stopReasonMaxTokens    = "max_tokens"
	stopReasonToolUse      = "tool_use"
	stopReasonStopSequence = "stop_sequence"
)

// ---- Usage ----

// usage holds token-usage fields from an Anthropic response or SSE event.
type usage struct {
	// InputTokens is the number of tokens in the prompt (input).
	InputTokens uint32 `json:"input_tokens"`
	// OutputTokens is the number of tokens in the generated response (output).
	OutputTokens uint32 `json:"output_tokens"`
}

// ---- Image source ----

type (
	// imageSource is a union of image source variants.
	imageSource struct {
		Base64 *imageSourceBase64
		URL    *imageSourceURL
	}

	// imageSourceBase64 is a base64-encoded image.
	// https://platform.claude.com/docs/en/api/messages#base64_image_source
	imageSourceBase64 struct {
		// Type is always "base64".
		Type string `json:"type"`
		// MediaType is the MIME type, e.g. "image/jpeg".
		MediaType string `json:"media_type"`
		// Data is the base64-encoded image bytes.
		Data string `json:"data"`
	}

	// imageSourceURL is a URL-referenced image.
	// https://platform.claude.com/docs/en/api/messages#url_image_source
	imageSourceURL struct {
		// Type is always "url".
		Type string `json:"type"`
		// URL is the image URL.
		URL string `json:"url"`
	}
)

const (
	imageSourceTypeBase64 = "base64"
	imageSourceTypeURL    = "url"
)

func (s *imageSource) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in image source")
	}
	switch typ {
	case imageSourceTypeBase64:
		var src imageSourceBase64
		if err := json.Unmarshal(data, &src); err != nil {
			return fmt.Errorf("failed to unmarshal base64 image source: %w", err)
		}
		s.Base64 = &src
	case imageSourceTypeURL:
		var src imageSourceURL
		if err := json.Unmarshal(data, &src); err != nil {
			return fmt.Errorf("failed to unmarshal URL image source: %w", err)
		}
		s.URL = &src
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (s imageSource) MarshalJSON() ([]byte, error) {
	if s.Base64 != nil {
		return json.Marshal(s.Base64)
	}
	if s.URL != nil {
		return json.Marshal(s.URL)
	}
	return nil, fmt.Errorf("image source must have a defined type")
}

// ---- Document source ----

type (
	// documentSource is a union of document source variants.
	// https://platform.claude.com/docs/en/api/messages#document_block_param
	documentSource struct {
		Base64PDF    *base64PDFSource
		PlainText    *plainTextSource
		URL          *urlPDFSource
		ContentBlock *contentBlockSource
	}

	// base64PDFSource is a base64-encoded PDF document.
	base64PDFSource struct {
		// Type is always "base64".
		Type string `json:"type"`
		// MediaType is always "application/pdf".
		MediaType string `json:"media_type"`
		// Data is the base64-encoded PDF data.
		Data string `json:"data"`
	}

	// plainTextSource is a plain text document.
	plainTextSource struct {
		// Type is always "text".
		Type string `json:"type"`
		// MediaType is always "text/plain".
		MediaType string `json:"media_type"`
		// Data is the plain text content.
		Data string `json:"data"`
	}

	// urlPDFSource is a PDF document from a URL.
	urlPDFSource struct {
		// Type is always "url".
		Type string `json:"type"`
		// URL is the URL of the PDF document.
		URL string `json:"url"`
	}

	// contentBlockSource is a document sourced from content blocks.
	contentBlockSource struct {
		// Type is always "content".
		Type string `json:"type"`
		// Content is either a string or an array of text/image blocks.
		Content contentBlockSourceContent `json:"content"`
	}

	// contentBlockSourceContent is either plain text or an array of blocks.
	contentBlockSourceContent struct {
		Text  string
		Array []contentBlockSourceItem
	}

	// contentBlockSourceItem is a single text or image block.
	contentBlockSourceItem struct {
		Text  *textBlockParam
		Image *imageBlockParam
	}
)

const (
	documentSourceTypeBase64  = "base64"
	documentSourceTypeText    = "text"
	documentSourceTypeURL     = "url"
	documentSourceTypeContent = "content"
)

func (s *documentSource) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in document source")
	}
	switch typ {
	case documentSourceTypeBase64:
		var src base64PDFSource
		if err := json.Unmarshal(data, &src); err != nil {
			return fmt.Errorf("failed to unmarshal base64 PDF source: %w", err)
		}
		s.Base64PDF = &src
	case documentSourceTypeText:
		var src plainTextSource
		if err := json.Unmarshal(data, &src); err != nil {
			return fmt.Errorf("failed to unmarshal plain text source: %w", err)
		}
		s.PlainText = &src
	case documentSourceTypeURL:
		var src urlPDFSource
		if err := json.Unmarshal(data, &src); err != nil {
			return fmt.Errorf("failed to unmarshal URL PDF source: %w", err)
		}
		s.URL = &src
	case documentSourceTypeContent:
		var src contentBlockSource
		if err := json.Unmarshal(data, &src); err != nil {
			return fmt.Errorf("failed to unmarshal content block source: %w", err)
		}
		s.ContentBlock = &src
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (s documentSource) MarshalJSON() ([]byte, error) {
	if s.Base64PDF != nil {
		return json.Marshal(s.Base64PDF)
	}
	if s.PlainText != nil {
		return json.Marshal(s.PlainText)
	}
	if s.URL != nil {
		return json.Marshal(s.URL)
	}
	if s.ContentBlock != nil {
		return json.Marshal(s.ContentBlock)
	}
	return nil, fmt.Errorf("document source must have a defined type")
}

func (c *contentBlockSourceContent) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.Text = text
		return nil
	}
	var array []contentBlockSourceItem
	if err := json.Unmarshal(data, &array); err == nil {
		c.Array = array
		return nil
	}
	return fmt.Errorf("content block source content must be either text or array")
}

func (c contentBlockSourceContent) MarshalJSON() ([]byte, error) {
	if c.Text != "" {
		return json.Marshal(c.Text)
	}
	if len(c.Array) > 0 {
		return json.Marshal(c.Array)
	}
	return nil, fmt.Errorf("content block source content must have either text or array")
}

func (item *contentBlockSourceItem) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in content block source item")
	}
	switch typ {
	case blockTypeText:
		var block textBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal text block in content block source: %w", err)
		}
		item.Text = &block
	case blockTypeImage:
		var block imageBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal image block in content block source: %w", err)
		}
		item.Image = &block
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (item contentBlockSourceItem) MarshalJSON() ([]byte, error) {
	if item.Text != nil {
		return json.Marshal(item.Text)
	}
	if item.Image != nil {
		return json.Marshal(item.Image)
	}
	return nil, fmt.Errorf("content block source item must have a defined type")
}

// ---- Tool result content ----

type (
	// toolResultContent is the content of a tool result block.
	// It can be a plain string or an array of content items.
	toolResultContent struct {
		Text  string
		Array []toolResultContentItem
	}

	// toolResultContentItem is a single item in a tool result content array.
	toolResultContentItem struct {
		Text         *textBlockParam
		Image        *imageBlockParam
		SearchResult *searchResultBlockParam
		Document     *documentBlockParam
	}
)

func (c *toolResultContent) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.Text = text
		return nil
	}
	var array []toolResultContentItem
	if err := json.Unmarshal(data, &array); err == nil {
		c.Array = array
		return nil
	}
	return fmt.Errorf("tool result content must be either text or array")
}

func (c toolResultContent) MarshalJSON() ([]byte, error) {
	if c.Text != "" {
		return json.Marshal(c.Text)
	}
	if len(c.Array) > 0 {
		return json.Marshal(c.Array)
	}
	return nil, fmt.Errorf("tool result content must have either text or array")
}

func (item *toolResultContentItem) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in tool result content item")
	}
	switch typ {
	case blockTypeText:
		var block textBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal text block in tool result: %w", err)
		}
		item.Text = &block
	case blockTypeImage:
		var block imageBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal image block in tool result: %w", err)
		}
		item.Image = &block
	case blockTypeSearchResult:
		var block searchResultBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal search result block in tool result: %w", err)
		}
		item.SearchResult = &block
	case blockTypeDocument:
		var block documentBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal document block in tool result: %w", err)
		}
		item.Document = &block
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (item toolResultContentItem) MarshalJSON() ([]byte, error) {
	if item.Text != nil {
		return json.Marshal(item.Text)
	}
	if item.Image != nil {
		return json.Marshal(item.Image)
	}
	if item.SearchResult != nil {
		return json.Marshal(item.SearchResult)
	}
	if item.Document != nil {
		return json.Marshal(item.Document)
	}
	return nil, fmt.Errorf("tool result content item must have a defined type")
}

// ---- Citations ----

// citationsConfigParam enables or disables citations for a block.
// https://platform.claude.com/docs/en/api/messages#citations_config_param
type citationsConfigParam struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// ---- Web search tool result ----

type (
	// webSearchToolResultContent is the content of a web search tool result.
	webSearchToolResultContent struct {
		Results []webSearchResult
		Error   *webSearchToolResultError
	}

	// webSearchResult is a single web search result.
	// https://platform.claude.com/docs/en/api/messages#web_search_result
	webSearchResult struct {
		// Type is always "web_search_result".
		Type string `json:"type"`
		// Title is the title of the web page.
		Title string `json:"title"`
		// URL is the URL of the web page.
		URL string `json:"url"`
		// EncryptedContent is the encrypted page content.
		EncryptedContent string `json:"encrypted_content"`
		// PageAge is an optional age indicator (e.g. "2 days ago").
		PageAge *string `json:"page_age,omitempty"`
	}

	// webSearchToolResultError is an error in a web search tool result.
	// https://platform.claude.com/docs/en/api/messages#web_search_tool_result_error
	webSearchToolResultError struct {
		// Type is always "web_search_tool_result_error".
		Type string `json:"type"`
		// ErrorCode is the error code.
		ErrorCode string `json:"error_code"`
	}
)

func (w *webSearchToolResultContent) UnmarshalJSON(data []byte) error {
	var results []webSearchResult
	if err := json.Unmarshal(data, &results); err == nil {
		w.Results = results
		return nil
	}
	var wsError webSearchToolResultError
	if err := json.Unmarshal(data, &wsError); err == nil {
		w.Error = &wsError
		return nil
	}
	return fmt.Errorf("web search tool result content must be an array of results or an error")
}

func (w webSearchToolResultContent) MarshalJSON() ([]byte, error) {
	if w.Results != nil {
		return json.Marshal(w.Results)
	}
	if w.Error != nil {
		return json.Marshal(w.Error)
	}
	return nil, fmt.Errorf("web search tool result content must have either results or an error")
}

// ---- Request content block param types ----

// cacheControl is kept as any for pass-through purposes.
// We don't need to inspect or construct cache control values for now.
// https://platform.claude.com/docs/en/api/messages#cache_control_ephemeral
type cacheControl = any

type (
	// textBlockParam is a text content block in a request.
	// https://platform.claude.com/docs/en/api/messages#text_block_param
	textBlockParam struct {
		// Type is always "text".
		Type string `json:"type"`
		// Text is the text content.
		Text string `json:"text"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
		// Citations are inline citations for this text.
		Citations []any `json:"citations,omitempty"`
	}

	// imageBlockParam is an image content block in a request.
	// https://platform.claude.com/docs/en/api/messages#image_block_param
	imageBlockParam struct {
		// Type is always "image".
		Type string `json:"type"`
		// Source carries the image data.
		Source imageSource `json:"source"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// documentBlockParam is a document content block in a request.
	// https://platform.claude.com/docs/en/api/messages#document_block_param
	documentBlockParam struct {
		// Type is always "document".
		Type string `json:"type"`
		// Source carries the document data.
		Source documentSource `json:"source"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
		// Citations configures citation generation for this document.
		Citations *citationsConfigParam `json:"citations,omitempty"`
		// Context is additional grounding context for the document.
		Context string `json:"context,omitempty"`
		// Title is an optional document title.
		Title string `json:"title,omitempty"`
	}

	// searchResultBlockParam is a search result content block in a request.
	// https://platform.claude.com/docs/en/api/messages#search_result_block_param
	searchResultBlockParam struct {
		// Type is always "search_result".
		Type string `json:"type"`
		// Content is the text content of the search result.
		Content []textBlockParam `json:"content"`
		// Source is the source URL or identifier.
		Source string `json:"source"`
		// Title is the title of the search result.
		Title string `json:"title"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
		// Citations configures citation generation for this result.
		Citations *citationsConfigParam `json:"citations,omitempty"`
	}

	// thinkingBlockParam is a thinking content block in a request.
	// https://platform.claude.com/docs/en/api/messages#thinking_block_param
	thinkingBlockParam struct {
		// Type is always "thinking".
		Type string `json:"type"`
		// Thinking is the model's internal reasoning text to replay.
		Thinking string `json:"thinking"`
		// Signature is the cryptographic signature for this block.
		Signature string `json:"signature"`
	}

	// redactedThinkingBlockParam is a redacted thinking block in a request.
	// https://platform.claude.com/docs/en/api/messages#redacted_thinking_block_param
	redactedThinkingBlockParam struct {
		// Type is always "redacted_thinking".
		Type string `json:"type"`
		// Data is the opaque encoded payload.
		Data string `json:"data"`
	}

	// toolUseBlockParam is a tool invocation to replay in a request.
	// https://platform.claude.com/docs/en/api/messages#tool_use_block_param
	toolUseBlockParam struct {
		// Type is always "tool_use".
		Type string `json:"type"`
		// ID is the unique identifier for this tool-use block.
		ID string `json:"id"`
		// Name is the tool function name.
		Name string `json:"name"`
		// Input holds the structured arguments for the tool call.
		Input map[string]any `json:"input"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// toolResultBlockParam is a tool result content block in a request.
	// https://platform.claude.com/docs/en/api/messages#tool_result_block_param
	toolResultBlockParam struct {
		// Type is always "tool_result".
		Type string `json:"type"`
		// ToolUseID references the tool_use block being answered.
		ToolUseID string `json:"tool_use_id"`
		// Content holds the result payload; string or array of content blocks.
		Content *toolResultContent `json:"content,omitempty"`
		// IsError marks the tool result as a failure.
		IsError bool `json:"is_error,omitempty"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// serverToolUseBlockParam is a server tool use content block in a request.
	// https://platform.claude.com/docs/en/api/messages#server_tool_use_block_param
	serverToolUseBlockParam struct {
		// Type is always "server_tool_use".
		Type string `json:"type"`
		// ID is the unique identifier for this block.
		ID string `json:"id"`
		// Name is the tool name (e.g. "web_search").
		Name string `json:"name"`
		// Input holds the structured arguments.
		Input map[string]any `json:"input"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// webSearchToolResultBlockParam is a web search tool result block in a request.
	// https://platform.claude.com/docs/en/api/messages#web_search_tool_result_block_param
	webSearchToolResultBlockParam struct {
		// Type is always "web_search_tool_result".
		Type string `json:"type"`
		// ToolUseID references the server_tool_use block.
		ToolUseID string `json:"tool_use_id"`
		// Content is the search results or an error.
		Content webSearchToolResultContent `json:"content"`
		// CacheControl controls prompt caching for this block.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}
)

// contentBlockParam is a union of all possible request content block types.
// https://platform.claude.com/docs/en/api/messages#body-messages-content
type contentBlockParam struct {
	Text                *textBlockParam
	Image               *imageBlockParam
	Document            *documentBlockParam
	SearchResult        *searchResultBlockParam
	Thinking            *thinkingBlockParam
	RedactedThinking    *redactedThinkingBlockParam
	ToolUse             *toolUseBlockParam
	ToolResult          *toolResultBlockParam
	ServerToolUse       *serverToolUseBlockParam
	WebSearchToolResult *webSearchToolResultBlockParam
}

func (m *contentBlockParam) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in message content block")
	}
	switch typ {
	case blockTypeText:
		var block textBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal text block: %w", err)
		}
		m.Text = &block
	case blockTypeImage:
		var block imageBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal image block: %w", err)
		}
		m.Image = &block
	case blockTypeDocument:
		var block documentBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal document block: %w", err)
		}
		m.Document = &block
	case blockTypeSearchResult:
		var block searchResultBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal search result block: %w", err)
		}
		m.SearchResult = &block
	case blockTypeThinking:
		var block thinkingBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal thinking block: %w", err)
		}
		m.Thinking = &block
	case blockTypeRedactedThinking:
		var block redactedThinkingBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal redacted thinking block: %w", err)
		}
		m.RedactedThinking = &block
	case blockTypeToolUse:
		var block toolUseBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal tool use block: %w", err)
		}
		m.ToolUse = &block
	case blockTypeToolResult:
		var block toolResultBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal tool result block: %w", err)
		}
		m.ToolResult = &block
	case blockTypeServerToolUse:
		var block serverToolUseBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal server tool use block: %w", err)
		}
		m.ServerToolUse = &block
	case blockTypeWebSearchToolResult:
		var block webSearchToolResultBlockParam
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal web search tool result block: %w", err)
		}
		m.WebSearchToolResult = &block
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (m contentBlockParam) MarshalJSON() ([]byte, error) {
	if m.Text != nil {
		return json.Marshal(m.Text)
	}
	if m.Image != nil {
		return json.Marshal(m.Image)
	}
	if m.Document != nil {
		return json.Marshal(m.Document)
	}
	if m.SearchResult != nil {
		return json.Marshal(m.SearchResult)
	}
	if m.Thinking != nil {
		return json.Marshal(m.Thinking)
	}
	if m.RedactedThinking != nil {
		return json.Marshal(m.RedactedThinking)
	}
	if m.ToolUse != nil {
		return json.Marshal(m.ToolUse)
	}
	if m.ToolResult != nil {
		return json.Marshal(m.ToolResult)
	}
	if m.ServerToolUse != nil {
		return json.Marshal(m.ServerToolUse)
	}
	if m.WebSearchToolResult != nil {
		return json.Marshal(m.WebSearchToolResult)
	}
	return nil, fmt.Errorf("content block param must have a defined type")
}

// ---- Message content ----

// messageContent is the content of a message: either a plain string or
// an array of typed content block params.
type messageContent struct {
	Text  string
	Array []contentBlockParam
}

func (m *messageContent) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		m.Text = text
		return nil
	}
	var array []contentBlockParam
	if err := json.Unmarshal(data, &array); err == nil {
		m.Array = array
		return nil
	}
	return fmt.Errorf("message content must be either text or array")
}

func (m messageContent) MarshalJSON() ([]byte, error) {
	if m.Text != "" {
		return json.Marshal(m.Text)
	}
	if len(m.Array) > 0 {
		return json.Marshal(m.Array)
	}
	return nil, fmt.Errorf("message content must have either text or array")
}

// ---- Messages ----

// message is a single message in the Anthropic messages array.
// Only "user" and "assistant" roles are valid; system is a top-level field.
type message struct {
	// Role is the participant role: "user" or "assistant".
	Role string `json:"role"`
	// Content is a plain string or array of typed content blocks.
	Content messageContent `json:"content"`
}

// ---- Tool definitions ----

type (
	// toolInputSchema describes the input schema for a custom tool.
	toolInputSchema struct {
		// Type is always "object".
		Type string `json:"type"`
		// Properties maps parameter names to their schemas.
		Properties map[string]any `json:"properties,omitempty"`
		// Required lists the required parameters.
		Required []string `json:"required,omitempty"`
	}

	// customTool is a user-defined tool.
	// https://platform.claude.com/docs/en/api/messages#tool
	customTool struct {
		// Type is always "custom".
		Type string `json:"type"`
		// Name is the unique identifier for the tool.
		Name string `json:"name"`
		// InputSchema describes the tool's input arguments.
		InputSchema any `json:"input_schema"`
		// CacheControl controls prompt caching for this tool.
		CacheControl cacheControl `json:"cache_control,omitempty"`
		// Description explains what the tool does.
		Description string `json:"description,omitempty"`
	}

	// bashTool is the computer-use bash tool.
	// https://platform.claude.com/docs/en/api/messages#tool_bash_20250124
	bashTool struct {
		// Type is always "bash_20250124".
		Type string `json:"type"`
		// Name is always "bash".
		Name string `json:"name"`
		// CacheControl controls prompt caching for this tool.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// textEditorTool20250124 is the text editor tool (v1).
	// https://platform.claude.com/docs/en/api/messages#tool_text_editor_20250124
	textEditorTool20250124 struct {
		// Type is always "text_editor_20250124".
		Type string `json:"type"`
		// Name is always "str_replace_editor".
		Name string `json:"name"`
		// CacheControl controls prompt caching for this tool.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// textEditorTool20250429 is the text editor tool (v2).
	// https://platform.claude.com/docs/en/api/messages#tool_text_editor_20250429
	textEditorTool20250429 struct {
		// Type is always "text_editor_20250429".
		Type string `json:"type"`
		// Name is always "str_replace_based_edit_tool".
		Name string `json:"name"`
		// CacheControl controls prompt caching for this tool.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// textEditorTool20250728 is the text editor tool (v3).
	// https://platform.claude.com/docs/en/api/messages#tool_text_editor_20250728
	textEditorTool20250728 struct {
		// Type is always "text_editor_20250728".
		Type string `json:"type"`
		// Name is always "str_replace_based_edit_tool".
		Name string `json:"name"`
		// MaxCharacters limits the number of characters to read or write.
		MaxCharacters *float64 `json:"max_characters,omitempty"`
		// CacheControl controls prompt caching for this tool.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// webSearchTool is the built-in web search tool.
	// https://platform.claude.com/docs/en/api/messages#web_search_tool_20250305
	webSearchTool struct {
		// Type is always "web_search_20250305".
		Type string `json:"type"`
		// Name is always "web_search".
		Name string `json:"name"`
		// AllowedDomains restricts search to these domains.
		AllowedDomains []string `json:"allowed_domains,omitempty"`
		// BlockedDomains excludes these domains from search results.
		BlockedDomains []string `json:"blocked_domains,omitempty"`
		// MaxUses limits the number of times this tool can be called.
		MaxUses *float64 `json:"max_uses,omitempty"`
		// UserLocation provides approximate user location for search.
		UserLocation *webSearchLocation `json:"user_location,omitempty"`
		// CacheControl controls prompt caching for this tool.
		CacheControl cacheControl `json:"cache_control,omitempty"`
	}

	// webSearchLocation is the approximate user location for web search.
	webSearchLocation struct {
		// Type is always "approximate".
		Type     string `json:"type"`
		City     string `json:"city,omitempty"`
		Country  string `json:"country,omitempty"`
		Region   string `json:"region,omitempty"`
		Timezone string `json:"timezone,omitempty"`
	}
)

// toolUnion is a union of all tool types.
// https://platform.claude.com/docs/en/api/messages#tool_union
type toolUnion struct {
	Tool               *customTool
	BashTool           *bashTool
	TextEditor20250124 *textEditorTool20250124
	TextEditor20250429 *textEditorTool20250429
	TextEditor20250728 *textEditorTool20250728
	WebSearchTool      *webSearchTool
}

const (
	toolTypeCustom             = "custom"
	toolTypeBash20250124       = "bash_20250124"
	toolTypeTextEditor20250124 = "text_editor_20250124"
	toolTypeTextEditor20250429 = "text_editor_20250429"
	toolTypeTextEditor20250728 = "text_editor_20250728"
	toolTypeWebSearch20250305  = "web_search_20250305"
)

func (t *toolUnion) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	typStr := toolTypeCustom
	if typ != "" {
		typStr = typ
	}
	switch typStr {
	case toolTypeCustom:
		var tool customTool
		if err := json.Unmarshal(data, &tool); err != nil {
			return fmt.Errorf("failed to unmarshal custom tool: %w", err)
		}
		t.Tool = &tool
	case toolTypeBash20250124:
		var tool bashTool
		if err := json.Unmarshal(data, &tool); err != nil {
			return fmt.Errorf("failed to unmarshal bash tool: %w", err)
		}
		t.BashTool = &tool
	case toolTypeTextEditor20250124:
		var tool textEditorTool20250124
		if err := json.Unmarshal(data, &tool); err != nil {
			return fmt.Errorf("failed to unmarshal text editor tool (20250124): %w", err)
		}
		t.TextEditor20250124 = &tool
	case toolTypeTextEditor20250429:
		var tool textEditorTool20250429
		if err := json.Unmarshal(data, &tool); err != nil {
			return fmt.Errorf("failed to unmarshal text editor tool (20250429): %w", err)
		}
		t.TextEditor20250429 = &tool
	case toolTypeTextEditor20250728:
		var tool textEditorTool20250728
		if err := json.Unmarshal(data, &tool); err != nil {
			return fmt.Errorf("failed to unmarshal text editor tool (20250728): %w", err)
		}
		t.TextEditor20250728 = &tool
	case toolTypeWebSearch20250305:
		var tool webSearchTool
		if err := json.Unmarshal(data, &tool); err != nil {
			return fmt.Errorf("failed to unmarshal web search tool: %w", err)
		}
		t.WebSearchTool = &tool
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (t toolUnion) MarshalJSON() ([]byte, error) {
	if t.Tool != nil {
		return json.Marshal(t.Tool)
	}
	if t.BashTool != nil {
		return json.Marshal(t.BashTool)
	}
	if t.TextEditor20250124 != nil {
		return json.Marshal(t.TextEditor20250124)
	}
	if t.TextEditor20250429 != nil {
		return json.Marshal(t.TextEditor20250429)
	}
	if t.TextEditor20250728 != nil {
		return json.Marshal(t.TextEditor20250728)
	}
	if t.WebSearchTool != nil {
		return json.Marshal(t.WebSearchTool)
	}
	return nil, fmt.Errorf("tool union must have a defined type")
}

// ---- Tool choice ----

type (
	// toolChoice is the tool-use policy for a request.
	// https://platform.claude.com/docs/en/api/messages#body-tool-choice
	toolChoice struct {
		Auto *toolChoiceAuto
		Any  *toolChoiceAny
		Tool *toolChoiceTool
		None *toolChoiceNone
	}

	// toolChoiceAuto lets the model decide whether to use tools.
	// https://platform.claude.com/docs/en/api/messages#tool_choice_auto
	toolChoiceAuto struct {
		// Type is always "auto".
		Type string `json:"type"`
		// DisableParallelToolUse, when true, limits the model to one tool call at a time.
		DisableParallelToolUse *bool `json:"disable_parallel_tool_use,omitempty"`
	}

	// toolChoiceAny forces the model to use any available tool.
	// https://platform.claude.com/docs/en/api/messages#tool_choice_any
	toolChoiceAny struct {
		// Type is always "any".
		Type string `json:"type"`
		// DisableParallelToolUse, when true, limits the model to one tool call at a time.
		DisableParallelToolUse *bool `json:"disable_parallel_tool_use,omitempty"`
	}

	// toolChoiceTool forces the model to use the specified tool.
	// https://platform.claude.com/docs/en/api/messages#tool_choice_tool
	toolChoiceTool struct {
		// Type is always "tool".
		Type string `json:"type"`
		// Name is the required tool to use.
		Name string `json:"name"`
		// DisableParallelToolUse, when true, limits the model to one tool call at a time.
		DisableParallelToolUse *bool `json:"disable_parallel_tool_use,omitempty"`
	}

	// toolChoiceNone prevents the model from using any tools.
	// https://platform.claude.com/docs/en/api/messages#tool_choice_none
	toolChoiceNone struct {
		// Type is always "none".
		Type string `json:"type"`
	}
)

const (
	toolChoiceTypeAuto = "auto"
	toolChoiceTypeAny  = "any"
	toolChoiceTypeTool = "tool"
	toolChoiceTypeNone = "none"
)

func (tc *toolChoice) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in tool choice")
	}
	switch typ {
	case toolChoiceTypeAuto:
		var c toolChoiceAuto
		if err := json.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("failed to unmarshal tool choice auto: %w", err)
		}
		tc.Auto = &c
	case toolChoiceTypeAny:
		var c toolChoiceAny
		if err := json.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("failed to unmarshal tool choice any: %w", err)
		}
		tc.Any = &c
	case toolChoiceTypeTool:
		var c toolChoiceTool
		if err := json.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("failed to unmarshal tool choice tool: %w", err)
		}
		tc.Tool = &c
	case toolChoiceTypeNone:
		var c toolChoiceNone
		if err := json.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("failed to unmarshal tool choice none: %w", err)
		}
		tc.None = &c
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (tc toolChoice) MarshalJSON() ([]byte, error) {
	if tc.Auto != nil {
		return json.Marshal(tc.Auto)
	}
	if tc.Any != nil {
		return json.Marshal(tc.Any)
	}
	if tc.Tool != nil {
		return json.Marshal(tc.Tool)
	}
	if tc.None != nil {
		return json.Marshal(tc.None)
	}
	return nil, fmt.Errorf("tool choice must have a defined type")
}

// ---- Thinking config ----

type (
	// thinkingConfig is the configuration for model thinking behavior.
	// https://platform.claude.com/docs/en/api/messages#body-thinking
	thinkingConfig struct {
		Enabled  *thinkingEnabled
		Disabled *thinkingDisabled
		Adaptive *thinkingAdaptive
	}

	// thinkingEnabled enables extended thinking with a token budget.
	thinkingEnabled struct {
		// Type is always "enabled".
		Type string `json:"type"`
		// BudgetTokens is the maximum number of thinking tokens (must be >= 1024 and < max_tokens).
		BudgetTokens float64 `json:"budget_tokens"`
	}

	// thinkingDisabled disables extended thinking.
	thinkingDisabled struct {
		// Type is always "disabled".
		Type string `json:"type"`
	}

	// thinkingAdaptive lets the model decide whether to use extended thinking.
	thinkingAdaptive struct {
		// Type is always "adaptive".
		Type string `json:"type"`
	}
)

const (
	thinkingConfigTypeEnabled  = "enabled"
	thinkingConfigTypeDisabled = "disabled"
	thinkingConfigTypeAdaptive = "adaptive"
)

func (t *thinkingConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in thinking config")
	}
	switch typ {
	case thinkingConfigTypeEnabled:
		var tc thinkingEnabled
		if err := json.Unmarshal(data, &tc); err != nil {
			return fmt.Errorf("failed to unmarshal thinking enabled: %w", err)
		}
		t.Enabled = &tc
	case thinkingConfigTypeDisabled:
		var tc thinkingDisabled
		if err := json.Unmarshal(data, &tc); err != nil {
			return fmt.Errorf("failed to unmarshal thinking disabled: %w", err)
		}
		t.Disabled = &tc
	case thinkingConfigTypeAdaptive:
		var tc thinkingAdaptive
		if err := json.Unmarshal(data, &tc); err != nil {
			return fmt.Errorf("failed to unmarshal thinking adaptive: %w", err)
		}
		t.Adaptive = &tc
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (t thinkingConfig) MarshalJSON() ([]byte, error) {
	if t.Enabled != nil {
		return json.Marshal(t.Enabled)
	}
	if t.Disabled != nil {
		return json.Marshal(t.Disabled)
	}
	if t.Adaptive != nil {
		return json.Marshal(t.Adaptive)
	}
	return nil, fmt.Errorf("thinking config must have a defined type")
}

// ---- System prompt ----

// systemPrompt is the system prompt: either a plain string or an array of text blocks.
type systemPrompt struct {
	Text  string
	Texts []textBlockParam
}

func (s *systemPrompt) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		s.Text = text
		return nil
	}
	var texts []textBlockParam
	if err := json.Unmarshal(data, &texts); err == nil {
		s.Texts = texts
		return nil
	}
	return fmt.Errorf("system prompt must be either string or array of text blocks")
}

func (s systemPrompt) MarshalJSON() ([]byte, error) {
	if s.Text != "" {
		return json.Marshal(s.Text)
	}
	if len(s.Texts) > 0 {
		return json.Marshal(s.Texts)
	}
	return nil, fmt.Errorf("system prompt must have either text or texts")
}

// ---- Request ----

// request is the Anthropic Messages API request body.
// https://docs.claude.com/en/api/messages
type request struct {
	// Model is the model identifier, e.g. "claude-opus-4-5".
	Model string `json:"model"`
	// Messages is the ordered conversation history.
	Messages []message `json:"messages"`
	// MaxTokens is the maximum number of output tokens to generate. Required.
	MaxTokens int `json:"max_tokens,omitempty"`
	// StopSequences are custom strings that halt generation when produced.
	StopSequences []string `json:"stop_sequences,omitempty"`
	// System is the system prompt to guide model behavior.
	System *systemPrompt `json:"system,omitempty"`
	// Temperature is the sampling temperature (0–1). Mutually exclusive with TopP.
	Temperature *float64 `json:"temperature,omitempty"`
	// Thinking configures extended thinking behavior.
	Thinking *thinkingConfig `json:"thinking,omitempty"`
	// ToolChoice controls which tool (if any) the model must use.
	ToolChoice *toolChoice `json:"tool_choice,omitempty"`
	// Tools lists the tools the model may call.
	Tools []toolUnion `json:"tools,omitempty"`
	// Stream, when true, requests a streaming SSE response.
	Stream bool `json:"stream,omitempty"`
	// TopP is the nucleus-sampling probability. Mutually exclusive with Temperature.
	TopP *float64 `json:"top_p,omitempty"`
	// TopK limits sampling to the top K tokens.
	TopK *int `json:"top_k,omitempty"`
}

// ---- Response content block types ----

type (
	// textBlock is a text content block in the response.
	// https://platform.claude.com/docs/en/api/messages#text_block
	textBlock struct {
		// Type is always "text".
		Type string `json:"type"`
		// Text is the generated text.
		Text string `json:"text"`
		// Citations are inline citations (if any).
		Citations []any `json:"citations,omitempty"`
	}

	// toolUseBlock is a tool invocation content block in the response.
	// https://platform.claude.com/docs/en/api/messages#tool_use_block
	toolUseBlock struct {
		// Type is always "tool_use".
		Type string `json:"type"`
		// ID is the unique identifier for this tool-use block.
		ID string `json:"id"`
		// Name is the tool function name.
		Name string `json:"name"`
		// Input holds the structured tool arguments produced by the model.
		Input map[string]any `json:"input"`
	}

	// thinkingBlock is a thinking content block in the response.
	// https://platform.claude.com/docs/en/api/messages#thinking_block
	thinkingBlock struct {
		// Type is always "thinking".
		Type string `json:"type"`
		// Thinking is the model's reasoning text.
		Thinking string `json:"thinking"`
		// Signature is the cryptographic signature for this block.
		Signature string `json:"signature,omitempty"`
	}

	// redactedThinkingBlock is a redacted thinking content block in the response.
	// https://platform.claude.com/docs/en/api/messages#redacted_thinking_block
	redactedThinkingBlock struct {
		// Type is always "redacted_thinking".
		Type string `json:"type"`
		// Data is the opaque encoded payload.
		Data string `json:"data"`
	}

	// serverToolUseBlock is a server tool use content block in the response.
	// https://platform.claude.com/docs/en/api/messages#server_tool_use_block
	serverToolUseBlock struct {
		// Type is always "server_tool_use".
		Type string `json:"type"`
		// ID is the unique identifier for this block.
		ID string `json:"id"`
		// Name is the tool name (e.g. "web_search").
		Name string `json:"name"`
		// Input holds the structured arguments.
		Input map[string]any `json:"input"`
	}

	// webSearchToolResultBlock is a web search tool result in the response.
	// https://platform.claude.com/docs/en/api/messages#web_search_tool_result_block
	webSearchToolResultBlock struct {
		// Type is always "web_search_tool_result".
		Type string `json:"type"`
		// ToolUseID references the server_tool_use block.
		ToolUseID string `json:"tool_use_id"`
		// Content is the search results or an error.
		Content webSearchToolResultContent `json:"content"`
	}
)

// messagesContentBlock is a union of all possible response content block types.
// https://platform.claude.com/docs/en/api/messages#response-content
type messagesContentBlock struct {
	Text             *textBlock
	Tool             *toolUseBlock
	Thinking         *thinkingBlock
	RedactedThinking *redactedThinkingBlock
	ServerToolUse    *serverToolUseBlock
	WebSearchResult  *webSearchToolResultBlock
}

func (m *messagesContentBlock) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse JSON object: %w", err)
	}
	typ, _ := raw["type"].(string)
	if typ == "" {
		return errors.New("missing type field in response content block")
	}
	switch typ {
	case blockTypeText:
		var block textBlock
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal text block: %w", err)
		}
		m.Text = &block
	case blockTypeToolUse:
		var block toolUseBlock
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal tool use block: %w", err)
		}
		m.Tool = &block
	case blockTypeThinking:
		var block thinkingBlock
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal thinking block: %w", err)
		}
		m.Thinking = &block
	case blockTypeRedactedThinking:
		var block redactedThinkingBlock
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal redacted thinking block: %w", err)
		}
		m.RedactedThinking = &block
	case blockTypeServerToolUse:
		var block serverToolUseBlock
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal server tool use block: %w", err)
		}
		m.ServerToolUse = &block
	case blockTypeWebSearchToolResult:
		var block webSearchToolResultBlock
		if err := json.Unmarshal(data, &block); err != nil {
			return fmt.Errorf("failed to unmarshal web search tool result block: %w", err)
		}
		m.WebSearchResult = &block
	default:
		// Ignore unknown types for forward compatibility.
	}
	return nil
}

func (m messagesContentBlock) MarshalJSON() ([]byte, error) {
	if m.Text != nil {
		return json.Marshal(m.Text)
	}
	if m.Tool != nil {
		return json.Marshal(m.Tool)
	}
	if m.Thinking != nil {
		return json.Marshal(m.Thinking)
	}
	if m.RedactedThinking != nil {
		return json.Marshal(m.RedactedThinking)
	}
	if m.ServerToolUse != nil {
		return json.Marshal(m.ServerToolUse)
	}
	if m.WebSearchResult != nil {
		return json.Marshal(m.WebSearchResult)
	}
	return nil, fmt.Errorf("response content block must have a defined type")
}

// ---- Response ----

// response is the Anthropic Messages API non-streaming response body.
// https://docs.claude.com/en/api/messages
type response struct {
	// ID is the unique identifier for this response.
	ID string `json:"id"`
	// Type is always "message".
	Type string `json:"type,omitempty"`
	// Role is always "assistant".
	Role string `json:"role,omitempty"`
	// Model is the model that handled the request.
	Model string `json:"model,omitempty"`
	// Content is the ordered list of content blocks produced by the model.
	Content []messagesContentBlock `json:"content"`
	// StopReason indicates why generation stopped.
	StopReason string `json:"stop_reason,omitempty"`
	// StopSequence is the stop sequence that triggered the stop, if any.
	StopSequence string `json:"stop_sequence,omitempty"`
	// Usage reports token consumption for this request.
	Usage usage `json:"usage"`
}

// ---- SSE internal event structs ----

type sseMessageStart struct {
	Message struct {
		ID           string `json:"id"`
		Model        string `json:"model"`
		Role         string `json:"role"`
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
		Usage        usage  `json:"usage"`
	} `json:"message"`
}

type sseBlockStart struct {
	Index        int                  `json:"index"`
	ContentBlock messagesContentBlock `json:"content_block"`
}

type sseBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		// Type is "text_delta", "input_json_delta", "thinking_delta", or "signature_delta".
		Type string `json:"type"`
		// Text carries incremental text (type: "text_delta").
		Text string `json:"text,omitempty"`
		// PartialJSON carries incremental tool-input JSON (type: "input_json_delta").
		PartialJSON string `json:"partial_json,omitempty"`
		// Thinking carries incremental thinking text (type: "thinking_delta").
		Thinking string `json:"thinking,omitempty"`
		// Signature carries the final thinking-block signature (type: "signature_delta").
		Signature string `json:"signature,omitempty"`
	} `json:"delta"`
}

type sseMessageDelta struct {
	Delta struct {
		StopReason   string `json:"stop_reason"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta"`
	Usage usage `json:"usage"`
}

// ---- llm.LLMRequest implementation ----

// llmRequest implements llm.LLMRequest for the Anthropic Messages API.
type llmRequest struct {
	raw request
}

func (r *llmRequest) GetModel() string { return r.raw.Model }
func (r *llmRequest) IsStream() bool   { return r.raw.Stream }
func (r *llmRequest) GetMaxTokens() *int {
	if r.raw.MaxTokens == 0 {
		return nil
	}
	return &r.raw.MaxTokens
}
func (r *llmRequest) GetTemperature() *float64 { return r.raw.Temperature }
func (r *llmRequest) GetTopP() *float64        { return r.raw.TopP }

func (r *llmRequest) GetMessages() []llm.LLMMessage {
	msgs := messagesToLLM(r.raw.Messages)
	if systemMsgs := extractSystem(r.raw.System); len(systemMsgs) > 0 {
		return append(systemMsgs, msgs...)
	}
	return msgs
}

func (r *llmRequest) GetTools() []llm.LLMTool {
	return toolsToLLM(r.raw.Tools)
}

func (r *llmRequest) GetToolChoice() *llm.LLMToolChoice {
	return toolChoiceToLLM(r.raw.ToolChoice)
}

func (r *llmRequest) ToJSON() ([]byte, error) {
	return json.Marshal(r.raw)
}

// parseRequest parses an Anthropic Messages API request body.
func parseRequest(body []byte) (llm.LLMRequest, error) {
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &llmRequest{raw: req}, nil
}

// ---- llm.LLMResponse implementation ----

// llmResponse implements llm.LLMResponse for the Anthropic Messages API.
type llmResponse struct {
	raw response
}

func (r *llmResponse) GetID() string    { return r.raw.ID }
func (r *llmResponse) GetModel() string { return r.raw.Model }

func (r *llmResponse) GetMessages() []llm.LLMMessage {
	contentParts, toolCalls := blocksToContent(r.raw.Content)
	return []llm.LLMMessage{{
		Role:      r.raw.Role,
		Content:   contentParts,
		ToolCalls: toolCalls,
	}}
}

func (r *llmResponse) GetStopReason() string {
	return stopReasonToFinishReason(r.raw.StopReason)
}

func (r *llmResponse) GetUsage() llm.LLMUsage {
	return usageToLLM(r.raw.Usage)
}

func (r *llmResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r.raw)
}

// parseResponse parses an Anthropic Messages API response body.
func parseResponse(body []byte) (llm.LLMResponse, error) {
	var resp response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &llmResponse{raw: resp}, nil
}

// ---- llm.LLMResponseChunk implementation ----

// llmResponseChunk implements llm.LLMResponseChunk for a single Anthropic SSE event.
type llmResponseChunk struct {
	eventType    string
	id           string
	model        string
	delta        *llm.LLMMessage
	finishReason string
	usage        llm.LLMUsage
	index        int
}

func (c *llmResponseChunk) GetID() string          { return c.id }
func (c *llmResponseChunk) GetModel() string       { return c.model }
func (c *llmResponseChunk) GetStopReason() string  { return c.finishReason }
func (c *llmResponseChunk) GetUsage() llm.LLMUsage { return c.usage }

func (c *llmResponseChunk) GetMessages() []llm.LLMMessage {
	if c.delta == nil {
		return nil
	}
	msg := *c.delta
	msg.Index = &c.index
	return []llm.LLMMessage{msg}
}

// ToEvent encodes the chunk as a complete Anthropic SSE event: "event: <type>\ndata: <json>\n\n".
func (c *llmResponseChunk) ToEvent() ([]byte, error) {
	data, err := c.jsonPayload()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(c.eventType)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes(), nil
}

// jsonPayload builds the JSON object for this chunk's event type.
func (c *llmResponseChunk) jsonPayload() ([]byte, error) {
	switch c.eventType {
	case "message_start":
		return json.Marshal(map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":      c.id,
				"type":    "message",
				"role":    "assistant",
				"model":   c.model,
				"content": []interface{}{},
				"usage": map[string]uint32{
					"input_tokens":  c.usage.InputTokens,
					"output_tokens": c.usage.OutputTokens,
				},
			},
		})
	case "content_block_delta":
		var deltaText string
		var hasToolCall bool
		if c.delta != nil {
			for _, part := range c.delta.Content {
				if part.Type == llm.ContentPartTypeText && part.Text != "" {
					deltaText = part.Text
					break
				}
			}
			hasToolCall = len(c.delta.ToolCalls) > 0
		}
		if c.delta != nil && deltaText != "" {
			return json.Marshal(map[string]interface{}{
				"type":  "content_block_delta",
				"index": c.index,
				"delta": map[string]string{
					"type": "text_delta",
					"text": deltaText,
				},
			})
		}
		if c.delta != nil && hasToolCall {
			return json.Marshal(map[string]interface{}{
				"type":  "content_block_delta",
				"index": c.index,
				"delta": map[string]string{
					"type":         "input_json_delta",
					"partial_json": c.delta.ToolCalls[0].Arguments,
				},
			})
		}
		return json.Marshal(map[string]interface{}{
			"type":  "content_block_delta",
			"index": c.index,
			"delta": map[string]string{"type": "text_delta", "text": ""},
		})
	case "message_delta":
		stopReason := finishReasonToStopReason(c.finishReason)
		return json.Marshal(map[string]interface{}{
			"type":  "message_delta",
			"delta": map[string]string{"stop_reason": stopReason},
			"usage": map[string]uint32{"output_tokens": c.usage.OutputTokens},
		})
	case "message_stop":
		return json.Marshal(map[string]string{"type": "message_stop"})
	default:
		return json.Marshal(map[string]string{"type": c.eventType})
	}
}

// parseChunk parses a single Anthropic SSE event.
func parseChunk(eventType string, data []byte) (llm.LLMResponseChunk, error) {
	chunk := &llmResponseChunk{eventType: eventType}
	switch eventType {
	case "message_start":
		var msg sseMessageStart
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		chunk.id = msg.Message.ID
		chunk.model = msg.Message.Model
		chunk.usage = usageToLLM(msg.Message.Usage)
		chunk.delta = &llm.LLMMessage{Role: msg.Message.Role}

	case "content_block_delta":
		var cbd sseBlockDelta
		if err := json.Unmarshal(data, &cbd); err != nil {
			return nil, err
		}
		chunk.index = cbd.Index
		switch cbd.Delta.Type {
		case "text_delta":
			chunk.delta = &llm.LLMMessage{Content: []llm.LLMContentPart{{Type: llm.ContentPartTypeText, Text: cbd.Delta.Text}}}
		case "input_json_delta":
			chunk.delta = &llm.LLMMessage{
				ToolCalls: []llm.LLMToolCall{{Arguments: cbd.Delta.PartialJSON}},
			}
		}

	case "message_delta":
		var md sseMessageDelta
		if err := json.Unmarshal(data, &md); err != nil {
			return nil, err
		}
		chunk.finishReason = stopReasonToFinishReason(md.Delta.StopReason)
		chunk.usage = usageToLLM(md.Usage)
	}
	return chunk, nil
}

var (
	sseEventPrefix = []byte("event: ")
	sseDataPrefix  = []byte("data: ")
)

// sseParser accumulates an Anthropic streaming SSE response into a final llm.LLMResponse.
type sseParser struct {
	buf          []byte
	done         bool
	currentEvent string

	// Accumulated state.
	id           string
	model        string
	role         string
	inputTokens  uint32
	outputTokens uint32
	stopReason   string
	stopSequence string

	// Content block accumulation.
	blocks      []*messagesContentBlock
	textByIndex map[int]string
	jsonByIndex map[int]string
}

func newSSEParser() *sseParser {
	return &sseParser{
		textByIndex: make(map[int]string),
		jsonByIndex: make(map[int]string),
	}
}

func (a *sseParser) Feed(data []byte) ([]llm.LLMResponseChunk, error) {
	if a.done {
		return nil, nil
	}
	a.buf = append(a.buf, data...)
	return a.parseEvents()
}

func (a *sseParser) parseEvents() ([]llm.LLMResponseChunk, error) {
	var chunks []llm.LLMResponseChunk
	for {
		idx := bytes.IndexByte(a.buf, '\n')
		if idx < 0 {
			return chunks, nil
		}
		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+1:]

		if bytes.HasPrefix(line, sseEventPrefix) {
			a.currentEvent = string(bytes.TrimPrefix(line, sseEventPrefix))
			continue
		}
		if bytes.HasPrefix(line, sseDataPrefix) {
			payload := bytes.TrimPrefix(line, sseDataPrefix)
			eventType := a.currentEvent
			a.currentEvent = ""
			if err := a.processEvent(eventType, payload); err != nil {
				return chunks, err
			}
			chunk, err := parseChunk(eventType, payload)
			if err != nil {
				return chunks, err
			}
			chunks = append(chunks, chunk)
		}
	}
}

func (a *sseParser) processEvent(eventType string, data []byte) error {
	switch eventType {
	case "message_start":
		var msg sseMessageStart
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("llm-proxy: failed to parse Anthropic message_start: %w", err)
		}
		a.id = msg.Message.ID
		a.model = msg.Message.Model
		a.role = msg.Message.Role
		a.inputTokens = msg.Message.Usage.InputTokens

	case "content_block_start":
		var cbs sseBlockStart
		if err := json.Unmarshal(data, &cbs); err != nil {
			return fmt.Errorf("llm-proxy: failed to parse Anthropic content_block_start: %w", err)
		}
		for len(a.blocks) <= cbs.Index {
			a.blocks = append(a.blocks, &messagesContentBlock{})
		}
		block := cbs.ContentBlock
		a.blocks[cbs.Index] = &block

	case "content_block_delta":
		var cbd sseBlockDelta
		if err := json.Unmarshal(data, &cbd); err != nil {
			return fmt.Errorf("llm-proxy: failed to parse Anthropic content_block_delta: %w", err)
		}
		switch cbd.Delta.Type {
		case "text_delta":
			a.textByIndex[cbd.Index] += cbd.Delta.Text
		case "input_json_delta":
			a.jsonByIndex[cbd.Index] += cbd.Delta.PartialJSON
		}

	case "message_delta":
		var md sseMessageDelta
		if err := json.Unmarshal(data, &md); err != nil {
			return fmt.Errorf("llm-proxy: failed to parse Anthropic message_delta: %w", err)
		}
		a.stopReason = md.Delta.StopReason
		a.stopSequence = md.Delta.StopSequence
		a.outputTokens = md.Usage.OutputTokens

	case "message_stop":
		a.done = true
	}
	return nil
}

func (a *sseParser) Finish() (llm.LLMResponse, error) {
	content := make([]messagesContentBlock, len(a.blocks))
	for i, block := range a.blocks {
		content[i] = *block
		switch {
		case block.Text != nil:
			if t, ok := a.textByIndex[i]; ok {
				content[i].Text = &textBlock{
					Type: block.Text.Type,
					Text: t,
				}
			}
		case block.Tool != nil:
			if j, ok := a.jsonByIndex[i]; ok {
				var inputMap map[string]any
				_ = json.Unmarshal([]byte(j), &inputMap)
				content[i].Tool = &toolUseBlock{
					Type:  block.Tool.Type,
					ID:    block.Tool.ID,
					Name:  block.Tool.Name,
					Input: inputMap,
				}
			}
		}
	}

	return &llmResponse{raw: response{
		ID:           a.id,
		Role:         a.role,
		Model:        a.model,
		Content:      content,
		StopReason:   a.stopReason,
		StopSequence: a.stopSequence,
		Usage: usage{
			InputTokens:  a.inputTokens,
			OutputTokens: a.outputTokens,
		},
	}}, nil
}

// ---- Factory ----

// factory implements llm.LLMFactory for the Anthropic Messages API.
type factory struct{}

func (f *factory) ParseRequest(body []byte) (llm.LLMRequest, error) {
	return parseRequest(body)
}

func (f *factory) ParseResponse(body []byte) (llm.LLMResponse, error) {
	return parseResponse(body)
}

func (f *factory) ParseChunk(data []byte) (llm.LLMResponseChunk, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse chunk JSON: %w", err)
	}
	eventType, _ := raw["type"].(string)
	return parseChunk(eventType, data)
}

func (f *factory) NewSSEParser() llm.SSEParser { return newSSEParser() }

func (f *factory) TransformRequest(req llm.LLMRequest) (llm.LLMRequest, error) {
	llmMsgs := req.GetMessages()
	converted := llmToMessages(llmMsgs)
	system := llmToSystem(llmMsgs)
	tools := llmToTools(req.GetTools())
	maxTokens := 0
	if mt := req.GetMaxTokens(); mt != nil {
		maxTokens = *mt
	}
	raw := request{
		Model:       req.GetModel(),
		Messages:    converted,
		MaxTokens:   maxTokens,
		System:      system,
		Stream:      req.IsStream(),
		Temperature: req.GetTemperature(),
		TopP:        req.GetTopP(),
	}
	if len(tools) > 0 {
		raw.Tools = tools
		raw.ToolChoice = llmToToolChoice(req.GetToolChoice())
	}
	return &llmRequest{raw: raw}, nil
}

func (f *factory) TransformResponse(resp llm.LLMResponse) (llm.LLMResponse, error) {
	msgs := resp.GetMessages()
	stopReason := finishReasonToStopReason(resp.GetStopReason())
	var blocks []messagesContentBlock
	if len(msgs) > 0 {
		m := msgs[0]
		for _, part := range m.Content {
			if part.Type == llm.ContentPartTypeText && part.Text != "" {
				blocks = append(blocks, messagesContentBlock{
					Text: &textBlock{Type: blockTypeText, Text: part.Text},
				})
			}
		}
		for _, tc := range m.ToolCalls {
			var inputMap map[string]any
			if len(tc.Arguments) > 0 {
				_ = json.Unmarshal([]byte(tc.Arguments), &inputMap)
			}
			blocks = append(blocks, messagesContentBlock{
				Tool: &toolUseBlock{
					Type:  blockTypeToolUse,
					ID:    tc.ID,
					Name:  tc.Name,
					Input: inputMap,
				},
			})
		}
	}
	u := resp.GetUsage()
	raw := response{
		ID:         resp.GetID(),
		Model:      resp.GetModel(),
		Role:       llm.RoleAssistant,
		Content:    blocks,
		StopReason: stopReason,
		Usage: usage{
			InputTokens:  u.InputTokens,
			OutputTokens: u.OutputTokens,
		},
	}
	return &llmResponse{raw: raw}, nil
}

func (f *factory) TransformChunk(chunk llm.LLMResponseChunk) (llm.LLMResponseChunk, error) {
	msgs := chunk.GetMessages()
	finishReason := chunk.GetStopReason()
	eventType := "content_block_delta"
	var msg *llm.LLMMessage
	if len(msgs) > 0 {
		msg = &msgs[0]
	}
	if msg != nil && msg.Role != "" {
		eventType = "message_start"
	}
	if finishReason != "" {
		eventType = "message_delta"
	}
	result := &llmResponseChunk{
		eventType:    eventType,
		id:           chunk.GetID(),
		model:        chunk.GetModel(),
		finishReason: finishReason,
		usage:        chunk.GetUsage(),
	}
	if msg != nil {
		result.delta = &llm.LLMMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			ToolCalls: msg.ToolCalls,
		}
	}
	return result, nil
}

// ---- Conversion helpers ----

// usageToLLM converts an usage to a canonical llm.LLMUsage.
func usageToLLM(u usage) llm.LLMUsage {
	return llm.LLMUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.InputTokens + u.OutputTokens,
	}
}

// messagesToLLM converts Anthropic messages to canonical llm.LLMMessage values.
func messagesToLLM(msgs []message) []llm.LLMMessage {
	result := make([]llm.LLMMessage, 0, len(msgs))
	for _, msg := range msgs {
		llmMsg := llm.LLMMessage{Role: msg.Role}
		hasToolResult := false

		if msg.Content.Text != "" {
			llmMsg.Content = []llm.LLMContentPart{{Type: llm.ContentPartTypeText, Text: msg.Content.Text}}
		} else {
			for _, b := range msg.Content.Array {
				switch {
				case b.Text != nil:
					llmMsg.Content = append(llmMsg.Content, llm.LLMContentPart{Type: llm.ContentPartTypeText, Text: b.Text.Text})
				case b.Image != nil && b.Image.Source.URL != nil:
					llmMsg.Content = append(llmMsg.Content, llm.LLMContentPart{Type: llm.ContentPartTypeImage, Image: &llm.LLMContentImage{
						URL: b.Image.Source.URL.URL,
					}})
				case b.ToolUse != nil:
					var args string
					if b.ToolUse.Input != nil {
						raw, _ := json.Marshal(b.ToolUse.Input)
						args = string(raw)
					}
					llmMsg.ToolCalls = append(llmMsg.ToolCalls, llm.LLMToolCall{
						ID:        b.ToolUse.ID,
						Type:      llm.ToolTypeFunction,
						Name:      b.ToolUse.Name,
						Arguments: args,
					})
				case b.ToolResult != nil:
					hasToolResult = true
					var resultText string
					if b.ToolResult.Content != nil {
						if b.ToolResult.Content.Text != "" {
							resultText = b.ToolResult.Content.Text
						} else if len(b.ToolResult.Content.Array) > 0 {
							for _, item := range b.ToolResult.Content.Array {
								if item.Text != nil {
									resultText = item.Text.Text
									break
								}
							}
						}
					}
					result = append(result, llm.LLMMessage{
						Role:       llm.RoleTool,
						ToolCallID: b.ToolResult.ToolUseID,
						Content:    []llm.LLMContentPart{{Type: llm.ContentPartTypeText, Text: resultText}},
					})
				}
			}
		}

		if !hasToolResult || len(llmMsg.Content) > 0 || len(llmMsg.ToolCalls) > 0 {
			result = append(result, llmMsg)
		}
	}
	return result
}

// toolsToLLM converts Anthropic tool definitions to canonical llm.LLMTool values.
func toolsToLLM(tools []toolUnion) []llm.LLMTool {
	result := make([]llm.LLMTool, 0, len(tools))
	for _, t := range tools {
		if t.Tool != nil {
			result = append(result, llm.LLMTool{
				Type:        llm.ToolTypeFunction,
				Name:        t.Tool.Name,
				Description: t.Tool.Description,
				Parameters:  t.Tool.InputSchema,
			})
		}
		// BashTool, TextEditorTool*, and WebSearchTool are provider-specific tools
		// that don't map cleanly to the canonical llm.LLMTool form; skip them.
	}
	return result
}

// extractSystem converts an Anthropic system prompt to canonical system LLMMessages.
func extractSystem(system *systemPrompt) []llm.LLMMessage {
	if system == nil {
		return nil
	}
	if system.Text != "" {
		return []llm.LLMMessage{{
			Role:    llm.RoleSystem,
			Content: []llm.LLMContentPart{{Type: llm.ContentPartTypeText, Text: system.Text}},
		}}
	}
	var msgs []llm.LLMMessage
	for _, tp := range system.Texts {
		if tp.Text != "" {
			msgs = append(msgs, llm.LLMMessage{
				Role:    llm.RoleSystem,
				Content: []llm.LLMContentPart{{Type: llm.ContentPartTypeText, Text: tp.Text}},
			})
		}
	}
	return msgs
}

// blocksToContent extracts content parts and tool calls from response content blocks.
func blocksToContent(blocks []messagesContentBlock) (contentParts []llm.LLMContentPart, toolCalls []llm.LLMToolCall) {
	for _, b := range blocks {
		switch {
		case b.Text != nil:
			contentParts = append(contentParts, llm.LLMContentPart{Type: llm.ContentPartTypeText, Text: b.Text.Text})
		case b.Tool != nil:
			var args string
			if b.Tool.Input != nil {
				raw, _ := json.Marshal(b.Tool.Input)
				args = string(raw)
			}
			toolCalls = append(toolCalls, llm.LLMToolCall{
				ID:        b.Tool.ID,
				Type:      llm.ToolTypeFunction,
				Name:      b.Tool.Name,
				Arguments: args,
			})
		}
	}
	return
}

// stopReasonToFinishReason maps Anthropic stop_reason values to canonical finish_reason strings.
func stopReasonToFinishReason(stopReason string) string {
	switch stopReason {
	case stopReasonEndTurn:
		return llm.FinishReasonStop
	case stopReasonMaxTokens:
		return llm.FinishReasonLength
	case stopReasonToolUse:
		return llm.FinishReasonToolCalls
	case stopReasonStopSequence:
		return llm.FinishReasonStop
	default:
		return stopReason
	}
}

// finishReasonToStopReason is the inverse of stopReasonToFinishReason.
func finishReasonToStopReason(finishReason string) string {
	switch finishReason {
	case llm.FinishReasonStop:
		return stopReasonEndTurn
	case llm.FinishReasonLength:
		return stopReasonMaxTokens
	case llm.FinishReasonToolCalls:
		return stopReasonToolUse
	default:
		return finishReason
	}
}

// toolChoiceToLLM converts an Anthropic tool choice to canonical llm.LLMToolChoice.
func toolChoiceToLLM(tc *toolChoice) *llm.LLMToolChoice {
	if tc == nil {
		return nil
	}
	if tc.Auto != nil {
		choice := &llm.LLMToolChoice{Type: llm.ToolChoiceAuto}
		if tc.Auto.DisableParallelToolUse != nil {
			choice.DisableParallelToolUse = *tc.Auto.DisableParallelToolUse
		}
		return choice
	}
	if tc.Any != nil {
		// Anthropic "any" maps to canonical "required".
		choice := &llm.LLMToolChoice{Type: llm.ToolChoiceRequired}
		if tc.Any.DisableParallelToolUse != nil {
			choice.DisableParallelToolUse = *tc.Any.DisableParallelToolUse
		}
		return choice
	}
	if tc.Tool != nil {
		choice := &llm.LLMToolChoice{Type: llm.ToolChoiceFunction, Name: tc.Tool.Name}
		if tc.Tool.DisableParallelToolUse != nil {
			choice.DisableParallelToolUse = *tc.Tool.DisableParallelToolUse
		}
		return choice
	}
	if tc.None != nil {
		return &llm.LLMToolChoice{Type: llm.ToolChoiceNone}
	}
	return nil
}

// llmToToolChoice converts a canonical llm.LLMToolChoice to an Anthropic tool choice.
func llmToToolChoice(tc *llm.LLMToolChoice) *toolChoice {
	if tc == nil {
		return nil
	}
	var disableParallel *bool
	if tc.DisableParallelToolUse {
		v := true
		disableParallel = &v
	}
	switch tc.Type {
	case llm.ToolChoiceRequired:
		return &toolChoice{Any: &toolChoiceAny{
			Type:                   toolChoiceTypeAny,
			DisableParallelToolUse: disableParallel,
		}}
	case llm.ToolChoiceFunction:
		return &toolChoice{Tool: &toolChoiceTool{
			Type:                   toolChoiceTypeTool,
			Name:                   tc.Name,
			DisableParallelToolUse: disableParallel,
		}}
	case llm.ToolChoiceNone:
		return &toolChoice{None: &toolChoiceNone{Type: toolChoiceTypeNone}}
	default: // auto
		return &toolChoice{Auto: &toolChoiceAuto{
			Type:                   toolChoiceTypeAuto,
			DisableParallelToolUse: disableParallel,
		}}
	}
}

// llmToMessages converts canonical llm.LLMMessage values to Anthropic message objects.
func llmToMessages(msgs []llm.LLMMessage) []message {
	result := make([]message, 0, len(msgs))
	i := 0
	for i < len(msgs) {
		msg := msgs[i]

		if msg.Role == llm.RoleSystem {
			i++
			continue
		}

		// Merge consecutive tool-result messages into a single user message.
		if msg.Role == llm.RoleTool {
			var toolResultBlocks []contentBlockParam
			for i < len(msgs) && msgs[i].Role == llm.RoleTool {
				m := msgs[i]
				var resultText string
				for _, part := range m.Content {
					if part.Type == llm.ContentPartTypeText {
						resultText = part.Text
						break
					}
				}
				content := &toolResultContent{Text: resultText}
				toolResultBlocks = append(toolResultBlocks, contentBlockParam{
					ToolResult: &toolResultBlockParam{
						Type:      blockTypeToolResult,
						ToolUseID: m.ToolCallID,
						Content:   content,
					},
				})
				i++
			}
			if len(toolResultBlocks) > 0 {
				result = append(result, message{
					Role:    llm.RoleUser,
					Content: messageContent{Array: toolResultBlocks},
				})
			}
			continue
		}

		var blocks []contentBlockParam
		for _, part := range msg.Content {
			if part.Type == llm.ContentPartTypeText {
				blocks = append(blocks, contentBlockParam{
					Text: &textBlockParam{Type: blockTypeText, Text: part.Text},
				})
			}
		}
		for _, tc := range msg.ToolCalls {
			var inputMap map[string]any
			if len(tc.Arguments) > 0 {
				_ = json.Unmarshal([]byte(tc.Arguments), &inputMap)
			}
			blocks = append(blocks, contentBlockParam{
				ToolUse: &toolUseBlockParam{
					Type:  blockTypeToolUse,
					ID:    tc.ID,
					Name:  tc.Name,
					Input: inputMap,
				},
			})
		}

		// Use a plain string for simple single-text-block messages.
		if len(blocks) == 1 && blocks[0].Text != nil {
			result = append(result, message{
				Role:    msg.Role,
				Content: messageContent{Text: blocks[0].Text.Text},
			})
		} else if len(blocks) > 0 {
			result = append(result, message{
				Role:    msg.Role,
				Content: messageContent{Array: blocks},
			})
		}
		i++
	}
	return result
}

// llmToSystem extracts the system prompt from msgs as an Anthropic system field.
func llmToSystem(msgs []llm.LLMMessage) *systemPrompt {
	for _, msg := range msgs {
		if msg.Role != llm.RoleSystem {
			continue
		}
		var texts []string
		for _, part := range msg.Content {
			if part.Type == llm.ContentPartTypeText && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		if len(texts) == 0 {
			continue
		}
		return &systemPrompt{Text: strings.Join(texts, "\n")}
	}
	return nil
}

// llmToTools converts canonical llm.LLMTool values to Anthropic tool union values.
func llmToTools(tools []llm.LLMTool) []toolUnion {
	result := make([]toolUnion, 0, len(tools))
	for _, t := range tools {
		result = append(result, toolUnion{
			Tool: &customTool{
				Type:        toolTypeCustom,
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			},
		})
	}
	return result
}

// NewFactory returns an llm.LLMFactory for the Anthropic Messages API.
func NewFactory() llm.LLMFactory { return &factory{} }
