// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the chat-completions-decoder filter.
package impl

import (
	"encoding/json"
	"fmt"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"
)

const defaultMetadataNamespace = "openai"

// chatCompletionsDecoderConfig holds the configuration for the decoder filter.
type chatCompletionsDecoderConfig struct {
	// MetadataNamespace is the filter metadata namespace under which the decoded
	// fields are stored. Defaults to "openai".
	MetadataNamespace string `json:"metadata_namespace"`
}

// decoderConfigFactory implements shared.HttpFilterConfigFactory.
type decoderConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *decoderConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	var cfg chatCompletionsDecoderConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			handle.Log(shared.LogLevelError, "chat-completions-decoder: failed to parse config: %s", err.Error())
			return nil, err
		}
	}
	if cfg.MetadataNamespace == "" {
		cfg.MetadataNamespace = defaultMetadataNamespace
	}
	return &decoderFilterFactory{config: &cfg}, nil
}

// decoderFilterFactory implements shared.HttpFilterFactory.
type decoderFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config *chatCompletionsDecoderConfig
}

func (f *decoderFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &decoderFilter{handle: handle, config: f.config}
}

// decoderFilter implements shared.HttpFilter.
type decoderFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *chatCompletionsDecoderConfig
}

func (f *decoderFilter) OnRequestHeaders(_ shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *decoderFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.decodeRequestBody()
	return shared.BodyStatusContinue
}

func (f *decoderFilter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	f.decodeRequestBody()
	return shared.TrailersStatusContinue
}

func (f *decoderFilter) OnResponseHeaders(_ shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *decoderFilter) OnResponseBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.decodeResponseBody()
	return shared.BodyStatusContinue
}

func (f *decoderFilter) OnResponseTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	f.decodeResponseBody()
	return shared.TrailersStatusContinue
}

// decodeRequestBody reads the request body, parses the OpenAI ChatCompletion request,
// and sets the structured information in filter metadata.
func (f *decoderFilter) decodeRequestBody() {
	bodyBytes := utility.ReadWholeRequestBody(f.handle)
	if len(bodyBytes) == 0 {
		return
	}

	decoded, err := decodeChatRequest(bodyBytes)
	if err != nil {
		f.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: failed to parse request: %s", err.Error())
		return
	}

	f.setRequestMetadata(f.config.MetadataNamespace, decoded)
}

// decodeResponseBody reads the response body, parses the OpenAI ChatCompletion response,
// and sets the structured information in filter metadata.
func (f *decoderFilter) decodeResponseBody() {
	bodyBytes := utility.ReadWholeResponseBody(f.handle)
	if len(bodyBytes) == 0 {
		return
	}

	decoded, err := decodeChatResponse(bodyBytes)
	if err != nil {
		f.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: failed to parse response: %s", err.Error())
		return
	}

	f.setResponseMetadata(f.config.MetadataNamespace, decoded)
}

// setRequestMetadata writes the decoded request fields into Envoy's dynamic filter metadata
// following the OpenInference Semantic Conventions.
func (f *decoderFilter) setRequestMetadata(namespace string, d *decodedRequest) {
	f.handle.SetMetadata(namespace, "llm.model_name", d.Model)
	f.handle.SetMetadata(namespace, "llm.system", "openai")
	f.handle.SetMetadata(namespace, "llm.input_messages.count", len(d.Messages))

	for i, msg := range d.Messages {
		f.handle.SetMetadata(namespace, fmt.Sprintf("llm.input_messages.%d.message.role", i), msg.Role)
		if content := extractContent(msg.Content); content != "" {
			f.handle.SetMetadata(namespace, fmt.Sprintf("llm.input_messages.%d.message.content", i), content)
		}
		if len(msg.ToolCalls) > 0 {
			f.handle.SetMetadata(namespace, fmt.Sprintf("llm.input_messages.%d.message.tool_calls.count", i), len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				f.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.input_messages.%d.message.tool_calls.%d.tool_call.id", i, j), tc.ID)
				f.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.input_messages.%d.message.tool_calls.%d.tool_call.function.name", i, j), tc.Function.Name)
				f.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.input_messages.%d.message.tool_calls.%d.tool_call.function.arguments", i, j), tc.Function.Arguments)
			}
		}
	}

	f.handle.SetMetadata(namespace, "llm.tools.count", len(d.Tools))
	for i, tool := range d.Tools {
		toolJSON, err := json.Marshal(tool)
		if err != nil {
			f.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: failed to marshal tool %d: %s", i, err.Error())
			continue
		}
		f.handle.SetMetadata(namespace, fmt.Sprintf("llm.tools.%d.tool.json_schema", i), string(toolJSON))
	}
}

// setResponseMetadata writes the decoded response fields into Envoy's dynamic filter metadata
// following the OpenInference Semantic Conventions.
func (f *decoderFilter) setResponseMetadata(namespace string, d *decodedResponse) {
	f.handle.SetMetadata(namespace, "llm.output_messages.count", len(d.Choices))
	for i, choice := range d.Choices {
		f.handle.SetMetadata(namespace, fmt.Sprintf("llm.output_messages.%d.message.role", i), choice.Message.Role)
		if content := extractContent(choice.Message.Content); content != "" {
			f.handle.SetMetadata(namespace, fmt.Sprintf("llm.output_messages.%d.message.content", i), content)
		}
		if len(choice.Message.ToolCalls) > 0 {
			f.handle.SetMetadata(namespace, fmt.Sprintf("llm.output_messages.%d.message.tool_calls.count", i), len(choice.Message.ToolCalls))
			for j, tc := range choice.Message.ToolCalls {
				f.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.output_messages.%d.message.tool_calls.%d.tool_call.id", i, j), tc.ID)
				f.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.output_messages.%d.message.tool_calls.%d.tool_call.function.name", i, j), tc.Function.Name)
				f.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.output_messages.%d.message.tool_calls.%d.tool_call.function.arguments", i, j), tc.Function.Arguments)
			}
		}
	}
	if d.Usage != nil {
		f.handle.SetMetadata(namespace, "llm.token_count.prompt", d.Usage.PromptTokens)
		f.handle.SetMetadata(namespace, "llm.token_count.completion", d.Usage.CompletionTokens)
		f.handle.SetMetadata(namespace, "llm.token_count.total", d.Usage.TotalTokens)
		if d.Usage.CompletionTokensDetails != nil {
			f.handle.SetMetadata(namespace, "llm.token_count.completion_details.reasoning", d.Usage.CompletionTokensDetails.ReasoningTokens)
			f.handle.SetMetadata(namespace, "llm.token_count.completion_details.audio", d.Usage.CompletionTokensDetails.AudioTokens)
		}
	}
}

// ExtensionName is the name used to refer to this plugin.
const ExtensionName = "chat-completions-decoder"

var wellKnownHTTPFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{
	ExtensionName: &decoderConfigFactory{},
}

// WellKnownHttpFilterConfigFactories returns the map of well-known HTTP filter config factories.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return wellKnownHTTPFilterConfigFactories
}
