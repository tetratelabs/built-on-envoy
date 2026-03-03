// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the chat-completions-decoder filter.
package impl

import (
	"encoding/json"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"
)

const defaultMetadataNamespace = "openai"

// chatCompletionsDecoderConfig holds the configuration for the decoder filter.
type chatCompletionsDecoderConfig struct {
	// MetadataNamespace is the filter metadata namespace under which the decoded
	// request fields are stored. Defaults to "openai".
	MetadataNamespace string `json:"metadata_namespace"`
}

func (c *chatCompletionsDecoderConfig) namespace() string {
	if c.MetadataNamespace != "" {
		return c.MetadataNamespace
	}
	return defaultMetadataNamespace
}

// decoderConfigFactory implements shared.HttpFilterConfigFactory.
type decoderConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *decoderConfigFactory) Create(
	handle shared.HttpFilterConfigHandle,
	config []byte,
) (shared.HttpFilterFactory, error) {
	var cfg chatCompletionsDecoderConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			handle.Log(shared.LogLevelError, "chat-completions-decoder: failed to parse config: %s", err.Error())
			return nil, err
		}
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

func (f *decoderFilter) OnRequestHeaders(
	_ shared.HeaderMap,
	endOfStream bool,
) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *decoderFilter) OnRequestBody(
	_ shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
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

	f.setMetadata(f.config.namespace(), decoded)
}

// setMetadata writes the decoded fields into Envoy's dynamic filter metadata.
func (f *decoderFilter) setMetadata(namespace string, d *decodedRequest) {
	f.handle.SetMetadata(namespace, "model", d.Model)
	f.handle.SetMetadata(namespace, "system_prompt", d.SystemPrompt)
	f.handle.SetMetadata(namespace, "user_prompt", d.UserPrompt)
	f.handle.SetMetadata(namespace, "message_count", int64(d.MessageCount))

	if d.HasTools {
		f.handle.SetMetadata(namespace, "has_tools", "true")
	} else {
		f.handle.SetMetadata(namespace, "has_tools", "false")
	}

	if d.HasToolCalls {
		f.handle.SetMetadata(namespace, "has_tool_calls", "true")
	} else {
		f.handle.SetMetadata(namespace, "has_tool_calls", "false")
	}

	if len(d.ToolNames) > 0 {
		toolNamesJSON, err := json.Marshal(d.ToolNames)
		if err == nil {
			f.handle.SetMetadata(namespace, "tool_names", string(toolNamesJSON))
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
