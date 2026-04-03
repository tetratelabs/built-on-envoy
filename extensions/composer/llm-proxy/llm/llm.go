// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package llm defines the shared interfaces, types, and constants for LLM API abstractions.
package llm

const (
	// KindOpenAI matches the OpenAI Chat Completions API.
	KindOpenAI string = "openai"
	// KindAnthropic matches the Anthropic Messages API.
	KindAnthropic string = "anthropic"
	// KindCustom matches a custom OpenAI-compatible API.
	// It uses the same request/response structure as OpenAI.
	KindCustom string = "custom"
)

// Message role constants.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Tool type constant.
const ToolTypeFunction = "function"

// Tool choice type constants.
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"
	ToolChoiceFunction = "function"
)

// Canonical finish reason constants (OpenAI-compatible values used throughout).
const (
	FinishReasonStop      = "stop"
	FinishReasonLength    = "length"
	FinishReasonToolCalls = "tool_calls"
)

// Content part type constants.
const (
	ContentPartTypeText    = "text"
	ContentPartTypeRefusal = "refusal"
	ContentPartTypeImage   = "image"
	ContentPartTypeAudio   = "audio"
	ContentPartTypeFile    = "file"
)

// LLMUsage holds the token-usage counters extracted from an LLM API response.
type LLMUsage struct {
	// InputTokens is the number of tokens consumed by the prompt / input.
	InputTokens uint32
	// OutputTokens is the number of tokens produced by the completion / output.
	OutputTokens uint32
	// TotalTokens is the sum of InputTokens and OutputTokens.
	TotalTokens uint32
}

// LLMContentAudio holds encoded audio data for an "input_audio" content part or an audio response.
type LLMContentAudio struct {
	// Data is the base64-encoded audio bytes.
	Data string
	// Format is the encoding format, e.g. "wav" or "mp3". Used for input audio.
	Format string
	// ID is the audio output identifier assigned by the provider. Set on response audio only.
	ID string
	// ExpiresAt is the Unix timestamp when the audio expires. Set on response audio only.
	ExpiresAt int64
	// Transcript is the text transcript of the audio. Set on response audio only.
	Transcript string
}

// LLMContentImage holds image data for an "image_url" content part.
type LLMContentImage struct {
	// URL is the image URL or a base64 data URI.
	URL string
}

// LLMContentFile holds file reference data for a "file" content part.
type LLMContentFile struct {
	// FileID is the ID of a previously uploaded file.
	FileID string
	// FileData is the base64-encoded file content.
	FileData string
	// Filename is the name of the file.
	Filename string
}

// LLMContentPart represents a single content item within an LLM message.
// Type determines the content kind (one of the ContentPartType* constants):
//   - ContentPartTypeText    → plain text (Text field)
//   - ContentPartTypeRefusal → model refusal text (Text field)
//   - ContentPartTypeImage   → image data (Image field)
//   - ContentPartTypeAudio   → encoded audio (Audio field)
//   - ContentPartTypeFile    → file reference (File field)
type LLMContentPart struct {
	// Type identifies the content kind; see ContentPartType* constants.
	Type string
	// Text holds plain text or refusal content.
	Text string
	// Image holds image data.
	Image *LLMContentImage
	// Audio holds encoded audio data.
	Audio *LLMContentAudio
	// File holds file reference data.
	File *LLMContentFile
}

// LLMMessage represents a single turn in an LLM conversation.
type LLMMessage struct {
	// Role is the participant role: "system", "user", "assistant", or "tool".
	Role string
	// Content holds the ordered list of content parts for this message.
	// Use ContentPartType* constants on each part's Type field to distinguish kinds.
	Content []LLMContentPart
	// ToolCalls holds tool invocations requested by the model in an assistant message.
	// May co-exist with Content.
	ToolCalls []LLMToolCall
	// ToolCallID is the ID of the tool call this message satisfies.
	// Set on "tool" role messages to correlate with a prior assistant tool call.
	ToolCallID string
	// Index is the 0-based choice index this message belongs to.
	// It is optional and only set on streaming chunks where the provider
	// returns multiple completion choices (e.g. OpenAI with n>1) or
	// identifies a specific content block index (e.g. Anthropic).
	Index *int
}

// LLMTool represents a callable function exposed to the model during generation.
type LLMTool struct {
	// Type is always "function".
	Type string
	// Name is the function identifier.
	Name string
	// Description explains what the function does.
	Description string
	// Parameters is a JSON Schema object describing the function's arguments.
	Parameters any
}

// LLMToolCall represents a single tool invocation requested by the model.
type LLMToolCall struct {
	// ID is a unique identifier for this call, used to correlate results.
	ID string
	// Type is always "function".
	Type string
	// Name is the name of the function to invoke.
	Name string
	// Arguments is the JSON-encoded argument string produced by the model.
	Arguments string
}

// LLMToolChoice represents the tool-selection policy for an LLM request.
type LLMToolChoice struct {
	// Type is "auto" (model decides), "none" (no tools), "required" (must use a tool),
	// or "tool" (use the specific tool named in Name).
	Type string
	// Name is the specific tool to call. Only used when Type is "tool".
	Name string
	// DisableParallelToolUse, when true, limits the model to at most one tool call.
	// Supported by Anthropic; ignored by providers that don't support it.
	DisableParallelToolUse bool
}

// LLMRequest abstracts over different LLM API request formats.
type LLMRequest interface {
	// GetModel returns the model name specified in the request.
	GetModel() string
	// GetMessages returns the ordered list of conversation messages.
	// System-role messages are included in-line so that callers need not
	// distinguish between providers that use a separate system field.
	GetMessages() []LLMMessage
	// GetTools returns the tools made available to the model.
	GetTools() []LLMTool
	// GetToolChoice returns the tool-selection policy, or nil when not set.
	GetToolChoice() *LLMToolChoice
	// IsStream returns whether the request asks for a streaming (SSE) response.
	IsStream() bool
	// GetMaxTokens returns the maximum number of tokens to generate, or nil if unset.
	GetMaxTokens() *int
	// GetTemperature returns the sampling temperature, or nil if unset.
	GetTemperature() *float64
	// GetTopP returns the nucleus-sampling probability, or nil if unset.
	GetTopP() *float64
	// ToJSON serializes the request to its vendor-specific API JSON format.
	ToJSON() ([]byte, error)
}

// LLMResponse abstracts over different LLM API non-streaming response formats.
type LLMResponse interface {
	// GetID returns the unique identifier for this response.
	GetID() string
	// GetModel returns the model that produced this response.
	GetModel() string
	// GetMessages returns the list of completion messages (one per choice).
	GetMessages() []LLMMessage
	// GetStopReason returns the stop/finish reason for the response
	// (e.g. "stop", "length", "tool_calls"), or "" if not available.
	GetStopReason() string
	// GetUsage returns token-usage information extracted from the response body.
	// The zero value of LLMUsage indicates that no usage data was present.
	GetUsage() LLMUsage
	// ToJSON serializes the response to its vendor-specific API JSON format.
	ToJSON() ([]byte, error)
}

// LLMResponseChunk abstracts over a single event in an LLM streaming SSE response.
type LLMResponseChunk interface {
	// GetID returns the unique identifier of the streaming response.
	GetID() string
	// GetModel returns the model producing this stream.
	GetModel() string
	// GetMessages returns the incremental message updates for this chunk.
	// Returns nil or empty slice when this chunk carries no message delta.
	// Multiple messages may be returned when the provider delivers updates for
	// several completion choices in a single event; use LLMMessage.Index to
	// distinguish them.
	GetMessages() []LLMMessage
	// GetStopReason returns the stop reason if this is the final content chunk,
	// or an empty string for intermediate chunks.
	GetStopReason() string
	// GetUsage returns token-usage information carried by this chunk.
	// The zero value of LLMUsage indicates that the chunk carries no usage data.
	GetUsage() LLMUsage
	// ToEvent encodes the chunk as a complete SSE event ready to write to the wire.
	// The returned bytes include the event framing (e.g. "data: ...\n\n" for OpenAI
	// or "event: ...\ndata: ...\n\n" for Anthropic).
	ToEvent() ([]byte, error)
}

// SSEParser incrementally consumes body chunks from an LLM streaming SSE response
// and produces an LLMResponse once the stream is complete.
type SSEParser interface {
	// Feed appends a new body chunk to the parser's internal buffer, processes
	// any complete SSE events it contains, and returns the parsed response chunks
	// and the first parse error encountered, if any.
	Feed(data []byte) ([]LLMResponseChunk, error)
	// Finish finalises parsing and returns the accumulated LLMResponse and any
	// terminal error encountered while processing the stream.
	Finish() (LLMResponse, error)
}

// LLMFactory creates the per-API-type parsers for a specific LLM provider and
// can transform canonical LLM objects into its own wire format.
type LLMFactory interface {
	// ParseRequest parses a complete request body and returns an LLMRequest.
	ParseRequest(body []byte) (LLMRequest, error)
	// ParseResponse parses a complete non-streaming response body and returns an LLMResponse.
	ParseResponse(body []byte) (LLMResponse, error)
	// ParseChunk parses a single streaming SSE event payload and returns an LLMResponseChunk.
	// For providers where the event type is separate from the payload (e.g. Anthropic), the
	// implementation is expected to extract the type from the JSON data itself.
	ParseChunk(data []byte) (LLMResponseChunk, error)
	// NewSSEParser creates an SSEParser for accumulating a streaming SSE response.
	NewSSEParser() SSEParser
	// TransformRequest converts any LLMRequest into one backed by this factory's
	// wire format, mapping all canonical fields to provider-specific equivalents.
	TransformRequest(req LLMRequest) (LLMRequest, error)
	// TransformResponse converts any LLMResponse into one backed by this factory's
	// wire format.
	TransformResponse(resp LLMResponse) (LLMResponse, error)
	// TransformChunk converts any LLMResponseChunk into one backed by this factory's
	// wire format.
	TransformChunk(chunk LLMResponseChunk) (LLMResponseChunk, error)
}
