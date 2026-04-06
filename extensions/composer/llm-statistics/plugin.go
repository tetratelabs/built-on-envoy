// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"
)

const (
	ExtensionName            = "llm-statistics"
	defaultMetadataNamespace = "io.builtonenvoy.llm-statistics"
	responseTypeStream       = "stream"
	responseTypeNonStream    = "nonstream"
)

type statisticsConfig struct {
	MetadataNamespace            string `json:"metadata_namespace"`
	UseDefaultAttributes         bool   `json:"use_default_attributes"`
	UseDefaultResponseAttributes bool   `json:"use_default_response_attributes"`
	SessionIDHeader              string `json:"session_id_header"`

	metrics statisticsMetrics `json:"-"`
}

type statisticsConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *statisticsConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	cfg := &statisticsConfig{}
	if len(config) > 0 {
		if err := json.Unmarshal(config, cfg); err != nil {
			handle.Log(shared.LogLevelError, "llm-statistics: failed to parse config: %s", err.Error())
			return nil, err
		}
	}
	if cfg.MetadataNamespace == "" {
		cfg.MetadataNamespace = defaultMetadataNamespace
	}
	cfg.metrics = newStatisticsMetrics(handle)
	handle.Log(shared.LogLevelInfo, "llm-statistics: initialized with namespace %q", cfg.MetadataNamespace)
	return &statisticsFilterFactory{config: cfg}, nil
}

type statisticsFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config *statisticsConfig
}

func (f *statisticsFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &statisticsFilter{
		handle:  handle,
		config:  f.config,
		metrics: f.config.metrics,
	}
}

type statisticsFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	config  *statisticsConfig
	metrics statisticsMetrics

	matched           bool
	parseFailed       bool
	streamParseFailed bool
	kind              string
	factory           LLMFactory
	model             string
	isStream          bool
	sseParser         SSEParser
	sessionID         string
	question          string
	system            string

	requestSentAt time.Time
	firstChunkAt  time.Time
}

func (f *statisticsFilter) OnRequestHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	pathBuf, _ := f.handle.GetAttributeString(shared.AttributeIDRequestPath)
	path := stripQueryString(pathBuf.ToUnsafeString())

	switch {
	case strings.HasSuffix(path, "/v1/chat/completions"):
		f.kind = KindOpenAI
		f.factory = &openAIFactory{}
	case strings.HasSuffix(path, "/v1/messages"):
		f.kind = KindAnthropic
		f.factory = &anthropicFactory{}
	default:
		return shared.HeadersStatusContinue
	}

	if endOfStream {
		return shared.HeadersStatusContinue
	}

	if ct := headers.GetOne("content-type").ToUnsafeString(); !strings.Contains(ct, "application/json") {
		return shared.HeadersStatusContinue
	}

	f.matched = true
	return shared.HeadersStatusStop
}

func (f *statisticsFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !f.matched {
		return shared.BodyStatusContinue
	}
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}

	body := utility.ReadWholeRequestBody(f.handle)
	req, err := f.factory.ParseRequest(body)
	if err != nil {
		f.metrics.requestsErrorIncrement(f.handle, f.kind, "", responseTypeNonStream)
		f.parseFailed = true
		f.handle.Log(shared.LogLevelDebug, "llm-statistics: failed to parse request: %s", err.Error())
		return shared.BodyStatusContinue
	}

	f.model = req.GetModel()
	f.isStream = req.IsStream()
	f.sessionID = f.extractSessionID()
	f.question = req.GetQuestion()
	f.system = req.GetSystem()
	f.requestSentAt = time.Now()

	responseType := responseTypeNonStream
	if f.isStream {
		responseType = responseTypeStream
	}
	f.handle.SetMetadata(f.config.MetadataNamespace, "kind", f.kind)
	f.handle.SetMetadata(f.config.MetadataNamespace, "model", f.model)
	f.handle.SetMetadata(f.config.MetadataNamespace, "response_type", responseType)
	if f.sessionID != "" {
		f.handle.SetMetadata(f.config.MetadataNamespace, "session_id", f.sessionID)
	}
	if f.question != "" {
		f.handle.SetMetadata(f.config.MetadataNamespace, "question", f.question)
	}
	if f.system != "" {
		f.handle.SetMetadata(f.config.MetadataNamespace, "system", f.system)
	}

	return shared.BodyStatusContinue
}

func (f *statisticsFilter) OnResponseHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if !f.matched || f.parseFailed {
		return shared.HeadersStatusContinue
	}
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	if strings.HasPrefix(headers.GetOne("content-type").ToUnsafeString(), "text/event-stream") {
		f.sseParser = f.factory.NewSSEParser()
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *statisticsFilter) OnResponseBody(body shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !f.matched || f.parseFailed {
		return shared.BodyStatusContinue
	}

	if f.sseParser != nil {
		if body != nil {
			for _, chunk := range body.GetChunks() {
				if f.streamParseFailed {
					break
				}
				if err := f.sseParser.Feed(chunk.ToBytes()); err != nil {
					f.streamParseFailed = true
					f.metrics.requestsErrorIncrement(f.handle, f.kind, f.model, responseTypeStream)
					f.handle.Log(shared.LogLevelDebug, "llm-statistics: failed to parse streaming response: %s", err.Error())
					break
				}
				if f.firstChunkAt.IsZero() && f.sseParser.SeenTextToken() {
					f.firstChunkAt = time.Now()
				}
			}
		}
		if endOfStream {
			if f.streamParseFailed {
				return shared.BodyStatusContinue
			}
			resp, err := f.sseParser.Finish()
			if err == nil {
				f.finish(resp, responseTypeStream)
			}
		}
		return shared.BodyStatusContinue
	}

	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.finalizeBufferedResponse()
	return shared.BodyStatusContinue
}

func (f *statisticsFilter) finalizeBufferedResponse() {
	if f.parseFailed {
		return
	}

	// Mark the buffered non-stream response as finalized so trailers/body end
	// callbacks cannot parse and record metrics twice.
	f.parseFailed = true

	resp, err := f.factory.ParseResponse(utility.ReadWholeResponseBody(f.handle))
	if err != nil {
		f.metrics.requestsErrorIncrement(f.handle, f.kind, f.model, responseTypeNonStream)
		f.handle.Log(shared.LogLevelDebug, "llm-statistics: failed to parse response: %s", err.Error())
		return
	}
	f.finish(resp, responseTypeNonStream)
}

func (f *statisticsFilter) OnResponseTrailers(trailers shared.HeaderMap) shared.HeadersStatus {
	if !f.matched || f.parseFailed || f.sseParser != nil {
		return shared.HeadersStatusContinue
	}

	f.finalizeBufferedResponse()
	return shared.HeadersStatusContinue
}
func (f *statisticsFilter) finish(resp LLMResponse, responseType string) {
	usage := resp.GetUsage()
	f.metrics.requestsIncrement(f.handle, f.kind, f.model, responseType)
	f.metrics.tokensIncrement(f.handle, f.kind, f.model, responseType, usage)

	f.handle.SetMetadata(f.config.MetadataNamespace, "input_token", int64(usage.InputTokens))
	f.handle.SetMetadata(f.config.MetadataNamespace, "output_token", int64(usage.OutputTokens))
	f.handle.SetMetadata(f.config.MetadataNamespace, "total_token", int64(usage.TotalTokens))
	if answer := resp.GetAnswer(); answer != "" {
		f.handle.SetMetadata(f.config.MetadataNamespace, "answer", answer)
	}
	if reasoning := resp.GetReasoning(); reasoning != "" {
		f.handle.SetMetadata(f.config.MetadataNamespace, "reasoning", reasoning)
	}
	if reasoningTokens := resp.GetReasoningTokens(); reasoningTokens > 0 {
		f.handle.SetMetadata(f.config.MetadataNamespace, "reasoning_tokens", int64(reasoningTokens))
	}
	if cachedTokens := resp.GetCachedTokens(); cachedTokens > 0 {
		f.handle.SetMetadata(f.config.MetadataNamespace, "cached_tokens", int64(cachedTokens))
	}
	toolCalls := resp.GetToolCalls()
	if toolCalls != nil {
		f.handle.SetMetadata(f.config.MetadataNamespace, "tool_calls", toolCalls)
	}
	if inputDetails := resp.GetInputTokenDetails(); inputDetails != nil {
		f.handle.SetMetadata(f.config.MetadataNamespace, "input_token_details", inputDetails)
	}
	if outputDetails := resp.GetOutputTokenDetails(); outputDetails != nil {
		f.handle.SetMetadata(f.config.MetadataNamespace, "output_token_details", outputDetails)
	}

	now := time.Now()
	if !f.requestSentAt.IsZero() {
		duration := now.Sub(f.requestSentAt).Milliseconds()
		if duration >= 0 {
			f.handle.SetMetadata(f.config.MetadataNamespace, "llm_service_duration_ms", duration)
			f.metrics.durationRecord(f.handle, f.kind, f.model, responseType, uint64(duration))
		}
	}
	if responseType == responseTypeStream && !f.requestSentAt.IsZero() && !f.firstChunkAt.IsZero() {
		first := f.firstChunkAt.Sub(f.requestSentAt).Milliseconds()
		if first >= 0 {
			f.handle.SetMetadata(f.config.MetadataNamespace, "llm_first_token_duration_ms", first)
			f.metrics.firstTokenRecord(f.handle, f.kind, f.model, responseType, uint64(first))
		}
	}

	if f.config.UseDefaultResponseAttributes || f.config.UseDefaultAttributes {
		entry := map[string]any{
			"kind":          f.kind,
			"model":         f.model,
			"response_type": responseType,
			"input_token":   usage.InputTokens,
			"output_token":  usage.OutputTokens,
			"total_token":   usage.TotalTokens,
		}
		if f.sessionID != "" {
			entry["session_id"] = f.sessionID
		}
		if f.config.UseDefaultAttributes {
			if f.question != "" {
				entry["question"] = f.question
			}
			if f.system != "" {
				entry["system"] = f.system
			}
			if answer := resp.GetAnswer(); answer != "" {
				entry["answer"] = answer
			}
			if reasoning := resp.GetReasoning(); reasoning != "" {
				entry["reasoning"] = reasoning
			}
			if reasoningTokens := resp.GetReasoningTokens(); reasoningTokens > 0 {
				entry["reasoning_tokens"] = reasoningTokens
			}
			if cachedTokens := resp.GetCachedTokens(); cachedTokens > 0 {
				entry["cached_tokens"] = cachedTokens
			}
			if toolCalls := resp.GetToolCalls(); len(toolCalls) > 0 {
				entry["tool_calls"] = toolCalls
			}
			if inputDetails := resp.GetInputTokenDetails(); inputDetails != nil {
				entry["input_token_details"] = inputDetails
			}
			if outputDetails := resp.GetOutputTokenDetails(); outputDetails != nil {
				entry["output_token_details"] = outputDetails
			}
		}
		if !f.requestSentAt.IsZero() {
			entry["llm_service_duration_ms"] = now.Sub(f.requestSentAt).Milliseconds()
		}
		if responseType == responseTypeStream && !f.requestSentAt.IsZero() && !f.firstChunkAt.IsZero() {
			entry["llm_first_token_duration_ms"] = f.firstChunkAt.Sub(f.requestSentAt).Milliseconds()
		}
		if payload, err := json.Marshal(entry); err == nil {
			f.handle.Log(shared.LogLevelInfo, "llm-statistics: %s", string(payload))
		}
	}
}

func (f *statisticsFilter) extractSessionID() string {
	if f.config.SessionIDHeader == "" {
		return ""
	}
	return f.handle.RequestHeaders().GetOne(f.config.SessionIDHeader).ToUnsafeString()
}

func stripQueryString(path string) string {
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		return path[:idx]
	}
	return path
}

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &statisticsConfigFactory{},
	}
}

func (m statisticsMetrics) requestsIncrement(handle shared.HttpFilterHandle, kind, model, responseType string) {
	handle.IncrementCounterValue(m.requestsTotal, 1, kind, model, responseType)
}

func (m statisticsMetrics) requestsErrorIncrement(handle shared.HttpFilterHandle, kind, model, responseType string) {
	handle.IncrementCounterValue(m.requestsError, 1, kind, model, responseType)
}

func (m statisticsMetrics) tokensIncrement(handle shared.HttpFilterHandle, kind, model, responseType string, usage LLMUsage) {
	handle.IncrementCounterValue(m.inputTokens, uint64(usage.InputTokens), kind, model, responseType)
	handle.IncrementCounterValue(m.outputTokens, uint64(usage.OutputTokens), kind, model, responseType)
	handle.IncrementCounterValue(m.totalTokens, uint64(usage.TotalTokens), kind, model, responseType)
}

func (m statisticsMetrics) durationRecord(handle shared.HttpFilterHandle, kind, model, responseType string, value uint64) {
	handle.RecordHistogramValue(m.duration, value, kind, model, responseType)
}

func (m statisticsMetrics) firstTokenRecord(handle shared.HttpFilterHandle, kind, model, responseType string, value uint64) {
	handle.RecordHistogramValue(m.firstToken, value, kind, model, responseType)
}
