// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Generated from https://app.stainless.com/api/spec/documented/openai/openapi.documented.yml

package impl

import "encoding/json"

// CreateChatCompletionRequest is the full request body for POST /v1/chat/completions.
//
// Generated from the OpenAI API specification (CreateChatCompletionRequest schema).
// The schema extends CreateModelResponseProperties via allOf; those shared fields
// are not reproduced here.
type CreateChatCompletionRequest struct {
	// Required fields.

	// Messages is the list of messages comprising the conversation so far.
	// Must contain at least one item.
	Messages []ChatCompletionRequestMessage `json:"messages"`
	// Model is the ID of the model to use, e.g. "gpt-4o" or "o3".
	Model string `json:"model"`

	// Optional fields.

	// Modalities specifies the output types the model should generate, e.g. ["text"] or ["text","audio"].
	Modalities []string `json:"modalities,omitempty"`
	// Verbosity controls the level of verbosity in the response.
	Verbosity *string `json:"verbosity,omitempty"`
	// ReasoningEffort sets the reasoning effort level for o-series models.
	ReasoningEffort *string `json:"reasoning_effort,omitempty"`
	// MaxCompletionTokens is an upper bound for the number of tokens in the
	// completion, including visible output tokens and reasoning tokens.
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`
	// FrequencyPenalty penalizes new tokens based on their existing frequency in
	// the text so far. Range: -2.0 to 2.0. Default: 0.
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	// PresencePenalty penalizes new tokens based on whether they already appear
	// in the text. Range: -2.0 to 2.0. Default: 0.
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`
	// WebSearchOptions enables and configures the web search tool.
	WebSearchOptions *WebSearchOptions `json:"web_search_options,omitempty"`
	// TopLogprobs is the number of most likely tokens to return at each token
	// position (0–20). Requires Logprobs to be true.
	TopLogprobs *int `json:"top_logprobs,omitempty"`
	// ResponseFormat specifies the format the model must output.
	// oneOf: ResponseFormatText | ResponseFormatJsonSchema | ResponseFormatJsonObject.
	// Discriminated by the "type" property.
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	// Audio contains parameters for audio output. Required when
	// Modalities includes "audio".
	Audio *ChatCompletionAudioParams `json:"audio,omitempty"`
	// Store controls whether to store the output for model distillation or evals.
	// Supports text and image inputs. Default: false.
	Store *bool `json:"store,omitempty"`
	// Stream enables server-sent events streaming of model response data.
	// Default: false.
	Stream *bool `json:"stream,omitempty"`
	// Stop is the stop sequence configuration.
	// oneOf: string | []string.
	Stop json.RawMessage `json:"stop,omitempty"`
	// LogitBias modifies the likelihood of specified tokens appearing in the
	// completion. Maps token IDs (as strings) to bias values from -100 to 100.
	LogitBias map[string]int `json:"logit_bias,omitempty"`
	// Logprobs controls whether to return log probabilities of output tokens.
	// Default: false.
	Logprobs *bool `json:"logprobs,omitempty"`
	// MaxTokens is the maximum number of tokens that can be generated.
	//
	// Deprecated: Use MaxCompletionTokens instead. Not compatible with o-series models.
	MaxTokens *int `json:"max_tokens,omitempty"`
	// N is the number of chat completion choices to generate for each input message.
	// Range: 1–128. Default: 1.
	N *int `json:"n,omitempty"`
	// Prediction configures a Predicted Output to improve response times when
	// large parts of the response are known ahead of time.
	// oneOf: PredictionContent.
	Prediction json.RawMessage `json:"prediction,omitempty"`
	// Seed requests deterministic sampling (beta). Refer to the system_fingerprint
	// response parameter to monitor backend changes.
	//
	// Deprecated: behaviour is best-effort only.
	Seed *int64 `json:"seed,omitempty"`
	// StreamOptions configures streaming behaviour. Only applicable when Stream is true.
	StreamOptions *ChatCompletionStreamOptions `json:"stream_options,omitempty"`
	// Tools is the list of tools the model may call.
	// Each item is oneOf: ChatCompletionTool | CustomToolChatCompletions.
	Tools []json.RawMessage `json:"tools,omitempty"`
	// ToolChoice controls which (if any) tool is called by the model.
	// oneOf: string ("none" | "auto" | "required") | ChatCompletionNamedToolChoice.
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
	// ParallelToolCalls controls whether the model may issue multiple tool calls
	// in parallel.
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
	// FunctionCall controls which (if any) function is called by the model.
	// oneOf: string ("none" | "auto") | ChatCompletionFunctionCallOption.
	//
	// Deprecated: Use ToolChoice instead.
	FunctionCall json.RawMessage `json:"function_call,omitempty"`
	// Functions is the list of functions the model may generate JSON inputs for.
	//
	// Deprecated: Use Tools instead. Range: 1–128 items.
	Functions []ChatCompletionFunction `json:"functions,omitempty"`
}

// ChatCompletionRequestMessage represents a single message in the conversation.
// The Content field may be either a plain string or an array of content parts,
// so it is kept as raw JSON for flexible unmarshalling.
type ChatCompletionRequestMessage struct {
	// Role is the author's role, e.g. "system", "user", "assistant", or "tool".
	Role string `json:"role"`
	// Content is the message content.
	// oneOf: string | []ContentPart.
	Content json.RawMessage `json:"content,omitempty"`
	// Name is an optional name for the participant.
	Name *string `json:"name,omitempty"`
	// ToolCallID is the ID of the tool call this message is responding to.
	// Required for role "tool".
	ToolCallID *string `json:"tool_call_id,omitempty"`
	// ToolCalls is the list of tool calls the model wants to make.
	// Only present for role "assistant".
	ToolCalls []json.RawMessage `json:"tool_calls,omitempty"`
}

// WebSearchOptions enables and configures the web search tool.
type WebSearchOptions struct {
	// UserLocation provides approximate location parameters for the search.
	UserLocation *WebSearchUserLocation `json:"user_location,omitempty"`
	// SearchContextSize controls how much web search context is retrieved.
	// oneOf values defined by WebSearchContextSize schema.
	SearchContextSize *string `json:"search_context_size,omitempty"`
}

// WebSearchUserLocation provides approximate location parameters for web search.
// Both fields are required when this object is present.
type WebSearchUserLocation struct {
	// Type is the type of location approximation. Always "approximate".
	Type string `json:"type"`
	// Approximate contains the location approximation data.
	Approximate json.RawMessage `json:"approximate"`
}

// ChatCompletionAudioParams contains parameters for audio output.
type ChatCompletionAudioParams struct {
	// Voice is the voice the model uses to respond.
	// Supported built-in values: alloy, ash, ballad, coral, echo, fable, nova,
	// onyx, sage, shimmer, marin, cedar. A custom voice object {"id": "..."} is
	// also accepted, so this field is kept as raw JSON.
	Voice json.RawMessage `json:"voice"`
	// Format is the output audio format.
	Format AudioFormat `json:"format"`
}

// AudioFormat specifies the output audio format for chat completion audio output.
type AudioFormat string

const (
	AudioFormatWav   AudioFormat = "wav"   // nolint:revive
	AudioFormatAAC   AudioFormat = "aac"   // nolint:revive
	AudioFormatMP3   AudioFormat = "mp3"   // nolint:revive
	AudioFormatFLAC  AudioFormat = "flac"  // nolint:revive
	AudioFormatOpus  AudioFormat = "opus"  // nolint:revive
	AudioFormatPCM16 AudioFormat = "pcm16" // nolint:revive
)

// ChatCompletionStreamOptions configures the behaviour of a streaming chat
// completion response.
type ChatCompletionStreamOptions struct {
	// IncludeUsage controls whether to include token usage statistics in the
	// final chunk of a streaming response.
	IncludeUsage *bool `json:"include_usage,omitempty"`
}

// ChatCompletionFunction is a function definition used with the deprecated
// Functions field.
type ChatCompletionFunction struct {
	// Name is the name of the function to call.
	Name string `json:"name"`
	// Description describes what the function does.
	Description *string `json:"description,omitempty"`
	// Parameters describes the function's parameters as a JSON Schema object.
	Parameters json.RawMessage `json:"parameters,omitempty"`
}
