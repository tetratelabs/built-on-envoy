// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package llmproxy implements an HTTP filter that identifies LLM API requests
// by matching the request path against configured rules, then extracts model,
// stream, and token-usage information and stores it as Envoy filter metadata.
//
// Two well-known API kinds are supported: OpenAI (Chat Completions) and
// Anthropic (Messages). Both streaming (SSE) and non-streaming JSON responses
// are handled. Requests whose path does not match any configured rule are
// passed through untouched.
package llmproxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const defaultMetadataNamespace = "io.builtonenvoy.llm-proxy"

var supportedAPIKinds = map[string]bool{
	KindOpenAI:    true,
	KindAnthropic: true,
	KindCustom:    true,
}

// llmConfig pairs a path matcher with a well-known LLM API kind.
type llmConfig struct {
	// Matcher describes how to test the incoming request path.
	// Exactly one of Prefix, Suffix, or Regex must be set.
	Matcher pkg.StringMatcher `json:"matcher"`
	// Kind is the well-known LLM API kind for requests matching this rule.
	// Accepted values: "openai", "anthropic", "custom".
	Kind string `json:"kind"`
	// Factory is the factory for parsing requests/responses for this rule; set during
	// filter initialization.
	Factory LLMFactory `json:"-"`
}

func (c *llmConfig) ValidateAndParse() error {
	// Validate the API kind.
	if !supportedAPIKinds[c.Kind] {
		return fmt.Errorf("llm-proxy: unsupported API kind %q", c.Kind)
	}

	// Validate the path matcher.
	if err := c.Matcher.ValidateAndParse(); err != nil {
		return err
	}

	// Set the LLMFactory based on the API kind.
	if c.Kind == KindOpenAI {
		c.Factory = &openaiFactory{}
	}
	if c.Kind == KindAnthropic {
		c.Factory = &anthropicFactory{}
	}
	// TODO(wbpcode): support custom format in the future.
	if c.Kind == KindCustom {
		c.Factory = &customFactory{}
	}
	return nil
}

// llmProxyConfig is the JSON configuration for the llm-proxy filter.
type llmProxyConfig struct {
	// LLMConfigs is the ordered list of path matchers and their associated API kinds.
	// LLMConfigs are evaluated in order; the first match wins.
	LLMConfigs []llmConfig `json:"llm_configs"`
	// MetadataNamespace is the filter-metadata namespace under which extracted
	// fields are stored. Defaults to "io.builtonenvoy.llm-proxy".
	MetadataNamespace string `json:"metadata_namespace"`
	// Header key to set the extracted model name if any.
	LLMModelHeader string `json:"llm_model_header"`
	// ClearRouteCache indicates whether to clear route cache to reselect route
	// based on the extracted model and metadata.
	// Only one of ClearRouteCache and ClearClusterCache can be true.
	ClearRouteCache bool `json:"clear_route_cache"`
	// Stats are defined at the config level and shared across all filter instances.
	stats llmProxyStats `json:"-"`
}

func (c *llmProxyConfig) ValidateAndParse() error {
	// Ensure at most only one rule is configured for well-known API kinds. It's fine to
	// have multiple rules with kind "custom" because we may support custom formats in the
	// future that require different parsing logic.
	var hasOpenAI, hasAnthropic bool
	for _, cfg := range c.LLMConfigs {
		if cfg.Kind == KindOpenAI {
			if hasOpenAI {
				return fmt.Errorf("llm-proxy: multiple rules with kind openai are not supported")
			}
			hasOpenAI = true
		}
		if cfg.Kind == KindAnthropic {
			if hasAnthropic {
				return fmt.Errorf("llm-proxy: multiple rules with kind anthropic are not supported")
			}
			hasAnthropic = true
		}
	}

	// If no rule is configured for openai, add a default one based on common OpenAI API paths.
	// This ensures basic functionality out of the box without requiring users to add config
	// for the most common case.
	if !hasOpenAI {
		c.LLMConfigs = append([]llmConfig{{
			Matcher: pkg.StringMatcher{Suffix: "/v1/chat/completions"},
			Kind:    KindOpenAI,
		}}, c.LLMConfigs...)
	}
	if !hasAnthropic {
		c.LLMConfigs = append([]llmConfig{{
			Matcher: pkg.StringMatcher{Suffix: "/v1/messages"},
			Kind:    KindAnthropic,
		}}, c.LLMConfigs...)
	}

	// Validate and parse each rule.
	for i := range c.LLMConfigs {
		if err := c.LLMConfigs[i].ValidateAndParse(); err != nil {
			return err
		}
	}

	// Check the metadata namespace is not empty.
	if c.MetadataNamespace == "" {
		c.MetadataNamespace = defaultMetadataNamespace
	}
	return nil
}

// llmProxyConfigFactory implements shared.HttpFilterConfigFactory.
type llmProxyConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *llmProxyConfigFactory) Create(handle shared.HttpFilterConfigHandle,
	config []byte,
) (shared.HttpFilterFactory, error) {
	cfg := &llmProxyConfig{}
	if len(config) > 0 {
		if err := json.Unmarshal(config, cfg); err != nil {
			handle.Log(shared.LogLevelError, "llm-proxy: failed to parse config: %s", err.Error())
			return nil, err
		}
	}
	if err := cfg.ValidateAndParse(); err != nil {
		handle.Log(shared.LogLevelError, "%s", err.Error())
		return nil, err
	}

	cfg.stats = newLLMProxyStats(handle)
	handle.Log(shared.LogLevelInfo, "llm-proxy: initialized with %d rules, namespace %q",
		len(cfg.LLMConfigs), cfg.MetadataNamespace)
	return &llmProxyFilterFactory{config: cfg}, nil
}

// llmProxyFilterFactory implements shared.HttpFilterFactory.
type llmProxyFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config *llmProxyConfig
}

func (f *llmProxyFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &llmProxyFilter{handle: handle, config: f.config}
}

// llmProxyFilter is the per-request HTTP filter instance.
type llmProxyFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *llmProxyConfig

	// matched is true when the request path matched one of the configured rules.
	matched bool
	// hasError is set when there was an error during request parsing.
	hasError bool

	// kind is the well-known API kind of the matched rule; empty when unmatched.
	kind string
	// factory is the LLMFactory resolved from the matched rule; nil when unmatched.
	factory LLMFactory

	// sseParser is non-nil when the response carries a streaming SSE body.
	// It accumulates parsed events incrementally as body chunks arrive.
	sseParser SSEParser

	// llmReq holds the parsed LLM request; set after the request body is processed.
	llmReq   LLMRequest
	model    string
	isStream bool

	// llmResp holds the parsed LLM response; set after the response body is processed.
	llmResp LLMResponse
	usage   LLMUsage

	// requestSentAt is the time at which the request body was successfully parsed and
	// released downstream; used to compute TTFT for streaming responses.
	requestSentAt time.Time
	// firstChunkAt is the time at which the first SSE body chunk arrived; used to
	// compute TTFT (firstChunkAt − requestSentAt) and TPOT for streaming responses.
	firstChunkAt time.Time
}

// matchRule returns the first rule whose Matcher matches path, or nil.
func (f *llmProxyFilter) matchRule(path string) *llmConfig {
	for i := range f.config.LLMConfigs {
		if f.config.LLMConfigs[i].Matcher.Matches(path) {
			return &f.config.LLMConfigs[i]
		}
	}
	return nil
}

func (f *llmProxyFilter) OnRequestHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	pathBuffer, _ := f.handle.GetAttributeString(shared.AttributeIDRequestPath)
	path := pathBuffer.ToUnsafeString()
	rule := f.matchRule(path)
	if rule == nil || rule.Factory == nil {
		// Unknown path: pass through without any processing.
		f.handle.Log(shared.LogLevelDebug, "llm-proxy: no matching valid rule found for path %q", path)
		return shared.HeadersStatusContinue
	}

	f.handle.Log(shared.LogLevelDebug, "llm-proxy: matched path %q to API kind %q", path, rule.Kind)

	// Now we have a matched API and related factory for parsing the request/response.
	// Set the flag so we can continue processing in the following callbacks.
	f.matched = true
	f.factory = rule.Factory
	f.kind = rule.Kind

	// The request path matches but this is headers only request which is unexpected for LLM APIs.
	if endOfStream {
		f.onError("headers-only request is not expected for LLM APIs")
		return shared.HeadersStatusContinue
	}

	// Check the content-type header to ensure it indicates a JSON body.
	if ct := headers.GetOne("content-type").ToUnsafeString(); !strings.Contains(ct, "application/json") {
		f.onError(fmt.Sprintf("unexpected request content-type %s", ct))
		return shared.HeadersStatusContinue
	}

	// Now, delay forwarding headers until the request body has been fully parsed.
	return shared.HeadersStatusStop
}

func (f *llmProxyFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !f.matched || f.hasError {
		return shared.BodyStatusContinue
	}
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.parseRequestBody()

	// We won't block the request anyway even there is an error during parsing.
	return shared.BodyStatusContinue
}

func (f *llmProxyFilter) OnRequestTrailers(shared.HeaderMap) shared.TrailersStatus {
	if !f.matched || f.hasError {
		return shared.TrailersStatusContinue
	}
	// When trailers are present Envoy does not set endOfStream on the last body
	// callback, so we finish parsing here if it hasn't happened already.
	f.parseRequestBody()

	// We won't block the request anyway even there is an error during parsing.
	return shared.TrailersStatusContinue
}

func (f *llmProxyFilter) OnResponseHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if !f.matched || f.hasError {
		return shared.HeadersStatusContinue
	}

	if endOfStream {
		f.onError("headers-only response is not expected for LLM APIs")
		return shared.HeadersStatusContinue
	}

	if st := headers.GetOne(":status").ToUnsafeString(); st != "200" {
		f.onError(fmt.Sprintf("unexpected response status %s", st))
		return shared.HeadersStatusContinue
	}

	ct := headers.GetOne("content-type").ToUnsafeString()
	if strings.Contains(ct, "text/event-stream") {
		f.handle.Log(shared.LogLevelDebug, "llm-proxy: handling SSE response")
		f.sseParser = f.factory.NewSSEParser()
		// Continue so that the response can stream through.
		return shared.HeadersStatusContinue
	}

	if !strings.Contains(ct, "application/json") {
		f.onError(fmt.Sprintf("unexpected response content-type %s", ct))
		return shared.HeadersStatusContinue
	}

	// Non-streaming: delay forwarding response headers until the full body is ready.
	return shared.HeadersStatusStop
}

func (f *llmProxyFilter) OnResponseBody(body shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !f.matched || f.hasError {
		return shared.BodyStatusContinue
	}

	// Record the time when the first chunk arrives even for non-streaming responses,
	// so that TTFT and TPOT can be computed consistently.
	if f.firstChunkAt.IsZero() {
		f.firstChunkAt = time.Now()
	}

	if f.sseParser != nil {
		// Streaming SSE: feed each arriving chunk into the parser.
		if body != nil {
			for _, chunk := range body.GetChunks() {
				if err := f.sseParser.Feed(chunk.ToUnsafeBytes()); err != nil {
					f.onError(fmt.Sprintf("event stream error: %s", err.Error()))
					return shared.BodyStatusContinue
				}
			}
		}
		if endOfStream {
			f.finishStreamingResponse()
		}
		return shared.BodyStatusContinue
	}
	// Non-streaming: buffer the entire response body before parsing.
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.parseResponseBody()
	return shared.BodyStatusContinue
}

func (f *llmProxyFilter) OnResponseTrailers(shared.HeaderMap) shared.TrailersStatus {
	// When trailers are present Envoy does not set endOfStream on the last body
	// callback, so we finish parsing here if it hasn't happened already.
	if !f.matched || f.hasError {
		return shared.TrailersStatusContinue
	}
	if f.sseParser != nil {
		f.finishStreamingResponse()
	} else {
		f.parseResponseBody()
	}
	return shared.TrailersStatusContinue
}

func (f *llmProxyFilter) onError(errorString string) {
	f.hasError = true

	f.handle.Log(
		shared.LogLevelDebug,
		"llm-proxy: error during request processing and skipping further parsing: %s",
		errorString,
	)
	f.handle.IncrementCounterValue(f.config.stats.requestError, 1, f.kind, f.model)

	// If the error happens before the request is down, let's update the stats to count the request
	// and avoid missing metrics.
	if f.requestSentAt.IsZero() {
		f.handle.IncrementCounterValue(f.config.stats.requestTotal, 1, f.kind, f.model)
	}
}

func (f *llmProxyFilter) onRequestSuccess() {
	f.requestSentAt = time.Now()

	ns := f.config.MetadataNamespace
	f.handle.SetMetadata(ns, "kind", f.kind)
	f.handle.SetMetadata(ns, "model", f.model)
	f.handle.SetMetadata(ns, "is_stream", f.isStream)

	f.handle.IncrementCounterValue(f.config.stats.requestTotal, 1, f.kind, f.model)

	if f.config.LLMModelHeader != "" {
		f.handle.RequestHeaders().Set(f.config.LLMModelHeader, f.model)
	}
	if f.config.ClearRouteCache {
		f.handle.ClearRouteCache()
	}
}

func (f *llmProxyFilter) onResponseSuccess() {
	ns := f.config.MetadataNamespace
	f.handle.SetMetadata(ns, "input_tokens", f.usage.InputTokens)
	f.handle.SetMetadata(ns, "output_tokens", f.usage.OutputTokens)
	f.handle.SetMetadata(ns, "total_tokens", f.usage.TotalTokens)

	// Set tokens stats. Token counts are always non-negative; clamp to 0 to satisfy
	// the static analyser before converting to uint64.
	f.handle.IncrementCounterValue(f.config.stats.inputTokens,
		uint64(f.usage.InputTokens), f.kind, f.model)
	f.handle.IncrementCounterValue(f.config.stats.outputTokens,
		uint64(f.usage.OutputTokens), f.kind, f.model)
	f.handle.IncrementCounterValue(f.config.stats.totalTokens,
		uint64(f.usage.TotalTokens), f.kind, f.model)

	// Handle some corner cases to avoid error.
	if f.requestSentAt.IsZero() || f.firstChunkAt.IsZero() || f.usage.OutputTokens == 0 {
		return
	}

	ttfp := f.firstChunkAt.Sub(f.requestSentAt).Milliseconds()
	tpot := time.Since(f.firstChunkAt).Milliseconds() / int64(f.usage.OutputTokens)

	f.handle.SetMetadata(ns, "request_ttft", ttfp)
	f.handle.SetMetadata(ns, "request_tpot", tpot)

	// nolint:gosec
	f.handle.RecordHistogramValue(f.config.stats.requestTTFT, uint64(max(ttfp, 0)), f.kind, f.model)
	// nolint:gosec
	f.handle.RecordHistogramValue(f.config.stats.requestTPOT, uint64(max(tpot, 0)), f.kind, f.model)
}

// parseRequestBody reads the complete request body, parses it via the matched
// LLMFactory, and writes model/stream metadata.
func (f *llmProxyFilter) parseRequestBody() {
	bodyBytes := utility.ReadWholeRequestBody(f.handle)
	if len(bodyBytes) == 0 {
		f.onError("empty request body is not expected for LLM APIs")
		return
	}
	req, err := f.factory.ParseRequest(bodyBytes)
	if err != nil || req == nil {
		f.onError(fmt.Sprintf("failed to parse request body: %v", err))
		return
	}
	f.model = req.GetModel()
	if f.model == "" {
		f.onError("model name is not specified in the request")
		return
	}
	f.isStream = req.IsStream()
	f.llmReq = req

	// Get the request correctly parsed and metadata set, we can set some metadata or stats now.
	f.onRequestSuccess()
}

// parseResponseBody reads the complete non-streaming response body, parses it
// via the matched LLMFactory, and writes usage metadata.
func (f *llmProxyFilter) parseResponseBody() {
	bodyBytes := utility.ReadWholeResponseBody(f.handle)
	if len(bodyBytes) == 0 {
		f.onError("empty response body is not expected for LLM APIs")
		return
	}
	resp, err := f.factory.ParseResponse(bodyBytes)
	if err != nil || resp == nil {
		f.onError(fmt.Sprintf("failed to parse response body: %v", err))
		return
	}
	f.llmResp = resp
	f.usage = resp.GetUsage()

	// Get the response correctly parsed and we can set metadata and stats now.
	f.onResponseSuccess()
}

// finishStreamingResponse finalises SSE parsing and writes usage metadata.
func (f *llmProxyFilter) finishStreamingResponse() {
	resp, err := f.sseParser.Finish()
	if err != nil || resp == nil {
		f.onError(fmt.Sprintf("failed to finish streaming response: %v", err))
		return
	}
	f.llmResp = resp
	f.usage = resp.GetUsage()

	// Get the response correctly parsed and we can set metadata and stats now.
	f.onResponseSuccess()
}

// ExtensionName is the name used to refer to this plugin in Envoy configuration.
const ExtensionName = "llm-proxy"

var wellKnownHTTPFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{
	ExtensionName: &llmProxyConfigFactory{},
}

// WellKnownHttpFilterConfigFactories returns the map of well-known HTTP filter config factories.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return wellKnownHTTPFilterConfigFactories
}
