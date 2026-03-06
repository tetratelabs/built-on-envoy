// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the chat-completions-decoder filter.
package impl

import (
	"encoding/json"
	"fmt"
	"strings"

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

func (d *decoderConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
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

	handle.Log(shared.LogLevelInfo, "chat-completions-decder: using metadata namespace %q", cfg.MetadataNamespace)

	return &decoderFilterFactory{config: &cfg}, nil
}

// decoderFilterFactory implements shared.HttpFilterFactory.
type decoderFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config *chatCompletionsDecoderConfig
}

func (d *decoderFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &decoderFilter{handle: handle, config: d.config}
}

// decoderFilter implements shared.HttpFilter.
type decoderFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *chatCompletionsDecoderConfig

	requestProcessed  bool // Guard to avoid processing the response body again in the request trailers.
	responseProcessed bool // Guard to avoid processing the response body again in the response trailers.
	// sseAcc is non-nil when the response is a streaming SSE response.
	// It accumulates parsed SSE events incrementally as body chunks arrive.
	sseAcc *sseAccumulator
}

func (d *decoderFilter) OnRequestHeaders(_ shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if !endOfStream {
		// If there is a body, we don't want to eagerly send the headers to the upstream until
		// we've parsed it. Stop header processing here. It will be resumed when the OnRequestBody
		// returns.
		return shared.HeadersStatusStop
	}
	return shared.HeadersStatusContinue
}

func (d *decoderFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !endOfStream {
		// Keep buffering the body until complete.
		return shared.BodyStatusStopAndBuffer
	}
	d.decodeRequestBody()
	return shared.BodyStatusContinue
}

func (d *decoderFilter) OnRequestTrailers(shared.HeaderMap) shared.TrailersStatus {
	// If the request had trailers, Envoy would have not set the `endOfStream` flag, so the
	// OnRequestBody method would have buffered the body but not processed it.
	// If that's the case, we process the body here.
	if !d.requestProcessed {
		d.decodeRequestBody()
	}
	return shared.TrailersStatusContinue
}

func (d *decoderFilter) OnResponseHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	// Detect streaming SSE responses by content-type so we can parse
	// chunks incrementally without buffering the entire response.
	if ct := headers.GetOne("content-type"); strings.HasPrefix(ct, "text/event-stream") {
		d.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: handling SSE response")
		d.sseAcc = newSSEAccumulator(func(format string, args ...any) {
			d.handle.Log(shared.LogLevelDebug, format, args...)
		})
		// Continue processing the response headers to leverage the response streaming
		return shared.HeadersStatusContinue
	}
	// If it's not a streaming response, we don't want to eagerly forward the response headers until
	// we have processed the response body. We stop the header processing here and it will be resumed
	// when the OnResponseBody returns.
	return shared.HeadersStatusStop
}

func (d *decoderFilter) OnResponseBody(body shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if d.sseAcc != nil { // Streaming SSE: feed each chunk incrementally.
		if body != nil {
			for _, chunk := range body.GetChunks() {
				d.sseAcc.feed(chunk)
			}
		}
		if endOfStream {
			d.decodeStreamingResponse()
		}
		return shared.BodyStatusContinue
	}

	// Non-streaming: buffer the entire response.
	if !endOfStream {
		// Keep buffering the body until complete.
		return shared.BodyStatusStopAndBuffer
	}
	d.decodeResponseBody()
	return shared.BodyStatusContinue
}

func (d *decoderFilter) OnResponseTrailers(shared.HeaderMap) shared.TrailersStatus {
	// If the response had trailers, Envoy would have not set the `endOfStream` flag, so the
	// OnResponseBody method would have buffered the body but not processed it.
	// If that's the case, we process the body here.
	if d.responseProcessed {
		return shared.TrailersStatusContinue
	}

	if d.sseAcc != nil {
		// Streaming SSE: complete processing the body.
		d.decodeStreamingResponse()
	} else {
		// Non-streaming: read the buffered body.
		d.decodeResponseBody()
	}

	return shared.TrailersStatusContinue
}

// decodeRequestBody reads the request body, parses the OpenAI ChatCompletion request,
// and sets the structured information in filter metadata.
func (d *decoderFilter) decodeRequestBody() {
	d.requestProcessed = true

	bodyBytes := utility.ReadWholeRequestBody(d.handle)
	if len(bodyBytes) == 0 {
		return
	}
	decoded, err := decodeChatRequest(bodyBytes)
	if err != nil {
		d.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: failed to parse request: %s", err.Error())
		return
	}

	d.setRequestMetadata(d.config.MetadataNamespace, decoded)
}

// decodeResponseBody reads the response body, parses the OpenAI ChatCompletion response,
// and sets the structured information in filter metadata.
func (d *decoderFilter) decodeResponseBody() {
	d.responseProcessed = true

	bodyBytes := utility.ReadWholeResponseBody(d.handle)
	if len(bodyBytes) == 0 {
		return
	}
	decoded, err := decodeChatResponse(bodyBytes)
	if err != nil {
		d.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: failed to parse response: %s", err.Error())
		return
	}

	d.setResponseMetadata(d.config.MetadataNamespace, decoded)
}

// decodeStreamingResponse completes the processing of the response body after having
// read all the SSE events.
func (d *decoderFilter) decodeStreamingResponse() {
	d.responseProcessed = true
	decoded := d.sseAcc.finish()
	d.setResponseMetadata(d.config.MetadataNamespace, decoded)
}

// setRequestMetadata writes the decoded request fields into Envoy's dynamic filter metadata
// following the OpenInference Semantic Conventions.
func (d *decoderFilter) setRequestMetadata(namespace string, req *chatCompletionRequest) {
	d.handle.SetMetadata(namespace, "llm.model_name", req.Model)
	d.handle.SetMetadata(namespace, "llm.system", "openai")
	d.handle.SetMetadata(namespace, "llm.input_messages.count", len(req.Messages))

	for i, msg := range req.Messages {
		d.handle.SetMetadata(namespace, fmt.Sprintf("llm.input_messages.%d.message.role", i), msg.Role)
		if content := extractContent(msg.Content); content != "" {
			d.handle.SetMetadata(namespace, fmt.Sprintf("llm.input_messages.%d.message.content", i), content)
		}
		if len(msg.ToolCalls) > 0 {
			d.handle.SetMetadata(namespace, fmt.Sprintf("llm.input_messages.%d.message.tool_calls.count", i), len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				d.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.input_messages.%d.message.tool_calls.%d.tool_call.id", i, j), tc.ID)
				d.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.input_messages.%d.message.tool_calls.%d.tool_call.function.name", i, j), tc.Function.Name)
				d.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.input_messages.%d.message.tool_calls.%d.tool_call.function.arguments", i, j), tc.Function.Arguments)
			}
		}
	}

	d.handle.SetMetadata(namespace, "llm.tools.count", len(req.Tools))
	for i, tool := range req.Tools {
		toolJSON, err := json.Marshal(tool)
		if err != nil {
			d.handle.Log(shared.LogLevelDebug, "chat-completions-decoder: failed to marshal tool %d: %s", i, err.Error())
			continue
		}
		d.handle.SetMetadata(namespace, fmt.Sprintf("llm.tools.%d.tool.json_schema", i), string(toolJSON))
	}
}

// setResponseMetadata writes the decoded response fields into Envoy's dynamic filter metadata
// following the OpenInference Semantic Conventions.
func (d *decoderFilter) setResponseMetadata(namespace string, resp *chatCompletionResponse) {
	d.handle.SetMetadata(namespace, "llm.output_messages.count", len(resp.Choices))
	for i, choice := range resp.Choices {
		d.handle.SetMetadata(namespace, fmt.Sprintf("llm.output_messages.%d.message.role", i), choice.Message.Role)
		if content := extractContent(choice.Message.Content); content != "" {
			d.handle.SetMetadata(namespace, fmt.Sprintf("llm.output_messages.%d.message.content", i), content)
		}
		if len(choice.Message.ToolCalls) > 0 {
			d.handle.SetMetadata(namespace, fmt.Sprintf("llm.output_messages.%d.message.tool_calls.count", i), len(choice.Message.ToolCalls))
			for j, tc := range choice.Message.ToolCalls {
				d.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.output_messages.%d.message.tool_calls.%d.tool_call.id", i, j), tc.ID)
				d.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.output_messages.%d.message.tool_calls.%d.tool_call.function.name", i, j), tc.Function.Name)
				d.handle.SetMetadata(namespace,
					fmt.Sprintf("llm.output_messages.%d.message.tool_calls.%d.tool_call.function.arguments", i, j), tc.Function.Arguments)
			}
		}
	}
	if resp.Usage != nil {
		d.handle.SetMetadata(namespace, "llm.token_count.prompt", resp.Usage.PromptTokens)
		d.handle.SetMetadata(namespace, "llm.token_count.completion", resp.Usage.CompletionTokens)
		d.handle.SetMetadata(namespace, "llm.token_count.total", resp.Usage.TotalTokens)
		if resp.Usage.CompletionTokensDetails != nil {
			d.handle.SetMetadata(namespace, "llm.token_count.completion_details.reasoning", resp.Usage.CompletionTokensDetails.ReasoningTokens)
			d.handle.SetMetadata(namespace, "llm.token_count.completion_details.audio", resp.Usage.CompletionTokensDetails.AudioTokens)
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
