// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the bedrock-guardrails extension.
package impl

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"
)

func getContent(bytes []byte) ([]Content, error) {
	prompt, err := ParseChatRequest(bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing chat request: %w", err)
	}
	// Build content data for the request
	var content []Content
	for _, t := range prompt {
		content = append(content, Content{
			Text: Text{Text: t},
		})
	}
	return content, nil
}

// This is the implementation of the HTTP filter.
type bedrockGuardrailsHTTPFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *bedrockGuardrailsConfig
}

func (f *bedrockGuardrailsHTTPFilter) OnRequestHeaders(_ shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if endOfStream {
		// TODO(wbpcode): this is header only request and we currently to continue processing.
		// But we may want to reject it in the future if the guardrail requires a body to work.
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: received header only request")
		return shared.HeadersStatusContinue
	}
	// Stop header processing as they might be modified in OnRequestBody and we may reject the request there
	// based on the body content
	return shared.HeadersStatusStop
}

func (f *bedrockGuardrailsHTTPFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: OnRequestBody called with endStream=%v", endOfStream)
	if !endOfStream {
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: buffering request body")
		return shared.BodyStatusStopAndBuffer
	}

	if !f.processRequestbody() {
		return shared.BodyStatusStopAndBuffer
	}

	return shared.BodyStatusContinue
}

func (f *bedrockGuardrailsHTTPFilter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if !f.processRequestbody() {
		return shared.TrailersStatusStop
	}
	return shared.TrailersStatusContinue
}

func (f *bedrockGuardrailsHTTPFilter) processRequestbody() bool {
	bodyBytes := utility.ReadWholeRequestBody(f.handle)
	if len(bodyBytes) == 0 {
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: no body provided, skipping")
		return true
	}
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: received request body: %s", string(bodyBytes))

	if len(f.config.BedrockGuardrails) == 0 {
		// TODO(wbpcode): we should reject the configuration without any guardrail,
		// but for now we just log and continue processing the request.
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: no guardrails configured, skipping")
		return true
	}

	// Clear content length header and body. The extension will fill it up again
	f.handle.RequestHeaders().Remove("content-length")
	f.handle.BufferedRequestBody().Drain(f.handle.BufferedRequestBody().GetSize())
	f.handle.ReceivedRequestBody().Drain(f.handle.ReceivedRequestBody().GetSize())

	// Trigger the first guardrail
	guardRail := f.config.BedrockGuardrails[0]
	args := &ApplyGuardrailArgs{
		GuardrailIdentifier: guardRail.Identifier,
		GuardrailVersion:    guardRail.Version,
		Body:                bodyBytes,
		Handle:              f.handle,
		Endpoint:            f.config.BedrockEndpoint,
		APIKey:              f.config.BedrockAPIKey,
	}
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: applying guardrail %s version %s", guardRail.Identifier, guardRail.Version)

	calloutHeaders, calloutBody, err := getCalloutHeaders(args)
	if err != nil {
		sendLocalRespError(f.handle, shared.LogLevelDebug, http.StatusBadGateway, fmt.Sprintf("error getting callout headers: %v", err.Error()), bodyBytes)
		return false
	}
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: got callout headers: %+v", calloutHeaders)
	result, cid := f.handle.HttpCallout(
		f.config.Cluster,
		calloutHeaders,
		calloutBody,
		1000*20, // 20sec default
		&applyGuardrailCallback{
			cfg:    f.config,
			handle: f.handle,
			body:   bodyBytes,
			index:  0,
		},
	)
	if result != shared.HttpCalloutInitSuccess {
		sendLocalRespError(f.handle, shared.LogLevelDebug, http.StatusBadGateway, fmt.Sprintf("error calling out: %v", result), bodyBytes)
		return false
	}
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: http callout sent ID: %v", cid)
	// Stop processing until the callout response is received and processed in the callback.
	return false
}

// This is the factory for the HTTP filter.
type customHTTPFilterFactory struct {
	config *bedrockGuardrailsConfig
}

func (f *customHTTPFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &bedrockGuardrailsHTTPFilter{handle: handle, config: f.config}
}

// OnDestroy implements EmptyHttpFilterConfigFactory
func (f *customHTTPFilterFactory) OnDestroy() {}

// CustomHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type CustomHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create creates a new instance of the HTTP filter factory with the given configuration.
func (f *CustomHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	// Parse JSON configuration
	handle.Log(shared.LogLevelDebug, "bedrock-guardrails: creating filter factory with config: %s", string(config))
	cfg := &bedrockGuardrailsConfig{
		TimeoutMs: 1000 * 10, // 10s default
	}
	if len(config) > 0 {
		if err := json.Unmarshal(config, cfg); err != nil {
			handle.Log(shared.LogLevelError, "bedrock-guardrails: failed to parse config: "+err.Error())
			return nil, err
		}
	}

	// Remove duplicated guardrails, if any
	cfg.BedrockGuardrails = dedupGuardrails(cfg.BedrockGuardrails)

	handle.Log(shared.LogLevelInfo, "bedrock-guardrails: loaded config: cluster=%s guardrails=%v", cfg.Cluster, cfg.BedrockGuardrails)
	return &customHTTPFilterFactory{config: cfg}, nil
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		"bedrock-guardrails": &CustomHttpFilterConfigFactory{},
	}
}

func dedupGuardrails(guardrails []bedrockGuardrail) []bedrockGuardrail {
	allKeys := make(map[string]bool)
	var list []bedrockGuardrail
	for _, guardrail := range guardrails {
		k := guardrail.Identifier + "#" + guardrail.Version
		if _, value := allKeys[k]; !value {
			allKeys[k] = true
			list = append(list, guardrail)
		}
	}
	return list
}
