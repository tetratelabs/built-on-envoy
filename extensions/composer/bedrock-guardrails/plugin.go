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

	requestBodyProcessed bool
}

func (f *bedrockGuardrailsHTTPFilter) OnRequestHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	// Stop header processing as they might be modified in OnRequestBody
	return shared.HeadersStatusStop
}

func (f *bedrockGuardrailsHTTPFilter) OnRequestBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: OnRequestBody called with endStream=%v", endStream)
	if !endStream {
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: buffering request body")
		return shared.BodyStatusStopAndBuffer
	}

	bodyBytes := utility.ReadWholeRequestBody(f.handle)
	if len(bodyBytes) == 0 {
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: no body provided, skipping")
		return shared.BodyStatusContinue
	}
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: received request body: %s", string(bodyBytes))

	if len(f.config.BedrockGuardrails) == 0 {
		f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: no guardrails configured, skipping")
		return shared.BodyStatusContinue
	}

	// Clear content length header and body. The extension will fill it up again
	f.handle.RequestHeaders().Remove("content-length")
	f.handle.BufferedRequestBody().Drain(f.handle.BufferedRequestBody().GetSize())

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
		return shared.BodyStatusContinue
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
		return shared.BodyStatusContinue
	}
	f.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: http callout sent ID: %v", cid)

	f.requestBodyProcessed = true
	return shared.BodyStatusStopAndBuffer
}

func (f *bedrockGuardrailsHTTPFilter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if f.requestBodyProcessed {
		return shared.TrailersStatusContinue
	}
	return shared.TrailersStatusStop
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
