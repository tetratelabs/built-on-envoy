// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the Azure Content Safety filter.
package impl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const (
	decisionAllowed   = "allowed"
	decisionBlocked   = "blocked"
	decisionMonitored = "monitored"
	decisionFailOpen  = "failopen"
	decisionError     = "error"
)

// contentSafetyMetrics holds the metric IDs for the Azure Content Safety filter.
type contentSafetyMetrics struct {
	requestsTotal shared.MetricID
	enabled       bool
}

func (m contentSafetyMetrics) inc(handle shared.HttpFilterHandle, decision string) {
	if m.enabled {
		handle.IncrementCounterValue(m.requestsTotal, 1, decision)
	}
}

// contentSafetyConfigFactory implements shared.HttpFilterConfigFactory.
type contentSafetyConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func parseConfig(unparsedConfig []byte,
	logFunc func(format string, args ...any),
) (*azureContentSafetyConfig, *azureContentSafetyClient, error) {
	if len(unparsedConfig) == 0 {
		// Allow empty config because route level config can be used to override the
		// global config, and in that case the global config could be empty.
		return nil, nil, nil
	}

	config := &azureContentSafetyConfig{}
	if err := json.Unmarshal(unparsedConfig, config); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if config.Endpoint == "" {
		return nil, nil, fmt.Errorf("endpoint is required")
	}
	if err := config.APIKey.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid 'api_key' configuration: %w", err)
	}

	apiKeyBytes, err := config.APIKey.Content()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get api key content: %w", err)
	}

	apiKey := strings.TrimSpace(string(apiKeyBytes))
	if config.Mode != "" && config.Mode != "block" && config.Mode != "monitor" {
		return nil, nil, fmt.Errorf("invalid mode %q", config.Mode)
	}

	client := newAzureContentSafetyClient(config.Endpoint, apiKey, config.apiVersion(), logFunc)
	return config, client, nil
}

func (f *contentSafetyConfigFactory) Create(
	handle shared.HttpFilterConfigHandle,
	unparsedConfig []byte,
) (shared.HttpFilterFactory, error) {
	logFunc := func(format string, args ...any) {
		handle.Log(shared.LogLevelDebug, format, args...)
	}

	config, client, err := parseConfig(unparsedConfig, logFunc)
	if err != nil {
		handle.Log(shared.LogLevelError, "azure-content-safety: %s", err.Error())
		return nil, err
	}

	if config == nil {
		handle.Log(shared.LogLevelInfo, "azure-content-safety: empty filter config")
	}

	// Define metrics even if config is empty because the route level config can be used at
	// runtime.
	var metrics contentSafetyMetrics
	metricID, metricStatus := handle.DefineCounter("azure_content_safety_requests_total", "decision")
	if metricStatus == shared.MetricsSuccess {
		metrics.requestsTotal = metricID
		metrics.enabled = true
	}

	return &contentSafetyFilterFactory{
		config:  config,
		client:  client,
		metrics: metrics,
	}, nil
}

// CreatePerRoute parses per-route configuration for the Azure Content Safety filter.
func (f *contentSafetyConfigFactory) CreatePerRoute(unparsedConfig []byte) (any, error) {
	config, client, err := parseConfig(unparsedConfig, nil)
	if err != nil {
		return nil, err
	}
	if config == nil || client == nil {
		// It's not allowed to have empty route config because it doesn't make sense to have an
		// empty config override the global config.
		return nil, fmt.Errorf("azure-content-safety: per-route config is empty or invalid")
	}
	return &perRouteAzureContentSafetyConfig{config: config, client: client}, nil
}

// contentSafetyFilterFactory implements shared.HttpFilterFactory.
type contentSafetyFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config  *azureContentSafetyConfig
	client  *azureContentSafetyClient
	metrics contentSafetyMetrics
}

// perRouteAzureContentSafetyConfig holds per-route configuration for the Azure Content Safety filter.
type perRouteAzureContentSafetyConfig struct {
	config *azureContentSafetyConfig
	client *azureContentSafetyClient
}

func (f *contentSafetyFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	config, client := f.config, f.client
	if perRoute := pkg.GetMostSpecificConfig[*perRouteAzureContentSafetyConfig](handle); perRoute != nil {
		config = perRoute.config
		client = perRoute.client
	}

	// If no any valid config is found, return an empty filter that just passes through the traffic.
	if config == nil || client == nil {
		handle.Log(shared.LogLevelInfo, "azure-content-safety: no config available and use empty filter")
		return &shared.EmptyHttpFilter{}
	}

	return &contentSafetyFilter{
		handle:  handle,
		config:  config,
		client:  client,
		metrics: &f.metrics,
	}
}

// contentSafetyFilter implements shared.HttpFilter.
type contentSafetyFilter struct {
	shared.EmptyHttpFilter
	handle                shared.HttpFilterHandle
	config                *azureContentSafetyConfig
	client                *azureContentSafetyClient
	metrics               *contentSafetyMetrics
	requestBodyProcessed  bool
	responseBodyProcessed bool
}

func (f *contentSafetyFilter) OnRequestHeaders(
	_ shared.HeaderMap,
	endOfStream bool,
) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *contentSafetyFilter) OnRequestBody(
	_ shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.requestBodyProcessed = true
	if f.processRequestBody() {
		return shared.BodyStatusContinue
	}
	return shared.BodyStatusStopAndBuffer
}

func (f *contentSafetyFilter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if f.requestBodyProcessed {
		return shared.TrailersStatusContinue
	}
	f.requestBodyProcessed = true
	if f.processRequestBody() {
		return shared.TrailersStatusContinue
	}
	return shared.TrailersStatusStop
}

// processRequestBody reads the buffered request body and starts analysis.
// Returns true if no async work is needed (caller should continue).
// Returns false if async work was launched (caller should stop and wait).
func (f *contentSafetyFilter) processRequestBody() bool {
	bodyBytes := utility.ReadWholeRequestBody(f.handle)
	if len(bodyBytes) == 0 {
		return true
	}

	reqFormat := detectRequestFormat(bodyBytes)
	if reqFormat == formatUnknown {
		f.handle.Log(shared.LogLevelInfo, "azure-content-safety: unrecognized request format, passing through")
		return true
	}
	p := parserForFormat(reqFormat)

	userPrompt, documents, err := p.ParseRequest(bodyBytes)
	if err != nil {
		f.handle.Log(shared.LogLevelInfo,
			"azure-content-safety: failed to parse %s request: %s", reqFormat, err.Error())
		return true
	}

	if userPrompt == "" {
		return true
	}

	// Azure API calls are blocking — run them off the Envoy worker thread.
	scheduler := f.handle.GetScheduler()
	go f.analyzeRequest(scheduler, userPrompt, documents, p, bodyBytes, reqFormat)
	return false
}

// analyzeRequest runs Azure API calls in a goroutine and schedules the result
// back to the Envoy worker thread.
func (f *contentSafetyFilter) analyzeRequest(
	scheduler shared.Scheduler,
	userPrompt string,
	documents []string,
	p Parser,
	bodyBytes []byte,
	reqFormat apiFormat,
) {
	result, err := f.client.ShieldPrompt(userPrompt, documents)
	if err != nil {
		scheduler.Schedule(func() {
			f.handleAPIErrorAsync("prompt shield", err, f.handle.ContinueRequest)
		})
		return
	}

	if isPromptAttackDetected(result) {
		scheduler.Schedule(func() {
			f.handle.Log(shared.LogLevelInfo, "azure-content-safety: prompt injection attack detected")
			if f.config.isBlockMode() {
				f.handle.SendLocalResponse(
					403,
					nil,
					[]byte("Request blocked: prompt injection detected"),
					"azure_content_safety_prompt_blocked",
				)
				f.metrics.inc(f.handle, decisionBlocked)
				return
			}
			f.handle.Log(shared.LogLevelInfo,
				"azure-content-safety: monitor mode - allowing request with detected prompt injection")
			f.metrics.inc(f.handle, decisionMonitored)
			f.handle.ContinueRequest()
		})
		return
	}

	// Task Adherence check (opt-in).
	if f.config.EnableTaskAdherence {
		taReq, parseErr := p.ParseRequestForTaskAdherence(bodyBytes)
		if parseErr != nil {
			scheduler.Schedule(func() {
				f.handle.Log(shared.LogLevelInfo,
					"azure-content-safety: failed to parse %s request for task adherence: %s", reqFormat, parseErr.Error())
				f.handleAPIErrorAsync("task adherence parse", parseErr, f.handle.ContinueRequest)
			})
			return
		}

		taResult, taErr := f.client.AnalyzeTaskAdherence(taReq, f.config.taskAdherenceAPIVersion())
		if taErr != nil {
			scheduler.Schedule(func() {
				f.handleAPIErrorAsync("task adherence", taErr, f.handle.ContinueRequest)
			})
			return
		}

		if taResult.TaskRiskDetected {
			scheduler.Schedule(func() {
				f.handle.Log(shared.LogLevelInfo,
					"azure-content-safety: task adherence risk detected: %s", taResult.Details)
				if f.config.isBlockMode() {
					f.handle.SendLocalResponse(
						403,
						nil,
						[]byte("Request blocked: task adherence risk detected"),
						"azure_content_safety_task_adherence_blocked",
					)
					f.metrics.inc(f.handle, decisionBlocked)
					return
				}
				f.handle.Log(shared.LogLevelInfo,
					"azure-content-safety: monitor mode - allowing request with task adherence risk")
				f.metrics.inc(f.handle, decisionMonitored)
				f.handle.ContinueRequest()
			})
			return
		}
	}

	scheduler.Schedule(func() {
		f.metrics.inc(f.handle, decisionAllowed)
		f.handle.ContinueRequest()
	})
}

func (f *contentSafetyFilter) OnResponseHeaders(
	_ shared.HeaderMap,
	endOfStream bool,
) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *contentSafetyFilter) OnResponseBody(
	_ shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	f.responseBodyProcessed = true
	if f.processResponseBody() {
		return shared.BodyStatusContinue
	}
	return shared.BodyStatusStopAndBuffer
}

func (f *contentSafetyFilter) OnResponseTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if f.responseBodyProcessed {
		return shared.TrailersStatusContinue
	}
	f.responseBodyProcessed = true
	if f.processResponseBody() {
		return shared.TrailersStatusContinue
	}
	return shared.TrailersStatusStop
}

// processResponseBody reads the buffered response body and starts analysis.
// Returns true if no async work is needed (caller should continue).
// Returns false if async work was launched (caller should stop and wait).
func (f *contentSafetyFilter) processResponseBody() bool {
	bodyBytes := utility.ReadWholeResponseBody(f.handle)
	if len(bodyBytes) == 0 {
		return true
	}

	respFormat := detectResponseFormat(bodyBytes)
	if respFormat == formatUnknown {
		f.handle.Log(shared.LogLevelInfo, "azure-content-safety: unrecognized response format, passing through")
		return true
	}

	content, err := parserForFormat(respFormat).ParseResponse(bodyBytes)
	if err != nil {
		f.handle.Log(shared.LogLevelInfo,
			"azure-content-safety: failed to parse %s response: %s", respFormat, err.Error())
		return true
	}

	if content == "" {
		return true
	}

	// Azure API calls are blocking — run them off the Envoy worker thread.
	scheduler := f.handle.GetScheduler()
	go f.analyzeResponse(scheduler, content)
	return false
}

// analyzeResponse runs Azure API calls in a goroutine and schedules the result
// back to the Envoy worker thread.
func (f *contentSafetyFilter) analyzeResponse(scheduler shared.Scheduler, content string) {
	result, err := f.client.AnalyzeText(content, f.config.categories())
	if err != nil {
		scheduler.Schedule(func() {
			f.handleAPIErrorAsync("text analysis", err, f.handle.ContinueResponse)
		})
		return
	}

	violations := f.checkThresholds(result)
	if len(violations) > 0 {
		scheduler.Schedule(func() {
			f.handle.Log(shared.LogLevelInfo,
				"azure-content-safety: harmful content detected: %s", strings.Join(violations, ", "))
			if f.config.isBlockMode() {
				f.handle.SendLocalResponse(
					403,
					nil,
					[]byte("Response blocked: harmful content detected"),
					"azure_content_safety_response_blocked",
				)
				f.metrics.inc(f.handle, decisionBlocked)
				return
			}
			f.handle.Log(shared.LogLevelInfo,
				"azure-content-safety: monitor mode - allowing response with detected harmful content")
			f.metrics.inc(f.handle, decisionMonitored)
			f.handle.ContinueResponse()
		})
		return
	}

	// Protected Material check (opt-in).
	if f.config.EnableProtectedMaterial {
		pmResult, pmErr := f.client.DetectProtectedMaterial(content)
		if pmErr != nil {
			scheduler.Schedule(func() {
				f.handleAPIErrorAsync("protected material", pmErr, f.handle.ContinueResponse)
			})
			return
		}

		if pmResult.ProtectedMaterialAnalysis != nil && pmResult.ProtectedMaterialAnalysis.Detected {
			scheduler.Schedule(func() {
				f.handle.Log(shared.LogLevelInfo, "azure-content-safety: protected material detected")
				if f.config.isBlockMode() {
					f.handle.SendLocalResponse(
						403,
						nil,
						[]byte("Response blocked: protected material detected"),
						"azure_content_safety_protected_material_blocked",
					)
					f.metrics.inc(f.handle, decisionBlocked)
					return
				}
				f.handle.Log(shared.LogLevelInfo,
					"azure-content-safety: monitor mode - allowing response with protected material")
				f.metrics.inc(f.handle, decisionMonitored)
				f.handle.ContinueResponse()
			})
			return
		}
	}

	scheduler.Schedule(func() {
		f.metrics.inc(f.handle, decisionAllowed)
		f.handle.ContinueResponse()
	})
}

func (f *contentSafetyFilter) checkThresholds(result *textAnalyzeResponse) []string {
	var violations []string
	for _, ca := range result.CategoriesAnalysis {
		if ca.Severity >= f.config.threshold(ca.Category) {
			violations = append(violations, fmt.Sprintf("%s(severity=%d)", ca.Category, ca.Severity))
		}
	}
	return violations
}

// handleAPIErrorAsync handles an Azure API error from an async goroutine.
// It either continues the stream (fail-open) or sends a 500 response (fail-closed).
func (f *contentSafetyFilter) handleAPIErrorAsync(apiName string, err error, continueStream func()) {
	if f.config.FailOpen {
		f.handle.Log(shared.LogLevelWarn,
			"azure-content-safety: %s API error (fail-open): %s", apiName, err.Error())
		f.metrics.inc(f.handle, decisionFailOpen)
		continueStream()
		return
	}
	f.handle.Log(shared.LogLevelError,
		"azure-content-safety: %s API error (fail-closed): %s", apiName, err.Error())
	f.handle.SendLocalResponse(
		500, nil,
		[]byte("Internal Server Error"),
		"azure_content_safety_api_error",
	)
	f.metrics.inc(f.handle, decisionError)
}

func isPromptAttackDetected(result *promptShieldResponse) bool {
	if result.UserPromptAnalysis != nil && result.UserPromptAnalysis.AttackDetected {
		return true
	}
	for _, doc := range result.DocumentsAnalysis {
		if doc.AttackDetected {
			return true
		}
	}
	return false
}

// ExtensionName is the name used to refer to this plugin.
const ExtensionName = "azure-content-safety"

var wellKnownHTTPFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{
	ExtensionName: &contentSafetyConfigFactory{},
}

// WellKnownHttpFilterConfigFactories returns the map of well-known HTTP filter config factories.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return wellKnownHTTPFilterConfigFactories
}
