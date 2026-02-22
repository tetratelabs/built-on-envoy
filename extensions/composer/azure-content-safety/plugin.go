// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the Azure Content Safety filter.
package impl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// resolveFileOrInline returns the value for a config field that supports both
// inline and file-based variants. If both are set, it returns an error.
// If filePath is set, the file contents are read and returned.
// Otherwise, inline is returned as-is.
func resolveFileOrInline(inline, filePath, fieldName string) (string, error) {
	if inline != "" && filePath != "" {
		return "", fmt.Errorf("cannot specify both %s and %s_file", fieldName, fieldName)
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath) //nolint:gosec // filePath is from trusted config, not user input
		if err != nil {
			return "", fmt.Errorf("failed to read %s_file %q: %w", fieldName, filePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return inline, nil
}

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

func (f *contentSafetyConfigFactory) Create(
	handle shared.HttpFilterConfigHandle,
	config []byte,
) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "azure-content-safety: empty config")
		return nil, fmt.Errorf("empty config")
	}

	var cfg azureContentSafetyConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "azure-content-safety: failed to parse config: %s", err.Error())
		return nil, err
	}

	if cfg.Endpoint == "" {
		handle.Log(shared.LogLevelError, "azure-content-safety: endpoint is required")
		return nil, fmt.Errorf("endpoint is required")
	}
	apiKey, err := resolveFileOrInline(cfg.APIKey, cfg.APIKeyFile, "api_key")
	if err != nil {
		handle.Log(shared.LogLevelError, "azure-content-safety: %s", err.Error())
		return nil, err
	}
	if apiKey == "" {
		handle.Log(shared.LogLevelError, "azure-content-safety: api_key or api_key_file is required")
		return nil, fmt.Errorf("api_key or api_key_file is required")
	}
	cfg.APIKey = apiKey
	if cfg.Mode != "" && cfg.Mode != "block" && cfg.Mode != "monitor" {
		handle.Log(shared.LogLevelError, "azure-content-safety: invalid mode %q, must be \"block\" or \"monitor\"", cfg.Mode)
		return nil, fmt.Errorf("invalid mode %q", cfg.Mode)
	}

	logFunc := func(format string, args ...any) {
		handle.Log(shared.LogLevelDebug, format, args...)
	}
	client := newAzureContentSafetyClient(cfg.Endpoint, cfg.APIKey, cfg.apiVersion(), logFunc)

	var metrics contentSafetyMetrics
	metricID, metricStatus := handle.DefineCounter("azure_content_safety_requests_total", "decision")
	if metricStatus == shared.MetricsSuccess {
		metrics.requestsTotal = metricID
		metrics.enabled = true
	}

	return &contentSafetyFilterFactory{
		config:  &cfg,
		client:  client,
		metrics: metrics,
	}, nil
}

// contentSafetyFilterFactory implements shared.HttpFilterFactory.
type contentSafetyFilterFactory struct {
	config  *azureContentSafetyConfig
	client  *azureContentSafetyClient
	metrics contentSafetyMetrics
}

func (f *contentSafetyFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &contentSafetyFilter{
		handle:  handle,
		config:  f.config,
		client:  f.client,
		metrics: &f.metrics,
	}
}

// contentSafetyFilter implements shared.HttpFilter.
type contentSafetyFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	config  *azureContentSafetyConfig
	client  *azureContentSafetyClient
	metrics *contentSafetyMetrics
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
	body shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}

	bodyBytes := readBodyBuffer(f.handle.BufferedRequestBody(), body)
	if len(bodyBytes) == 0 {
		return shared.BodyStatusContinue
	}

	reqFormat := detectRequestFormat(bodyBytes)
	if reqFormat == formatUnknown {
		f.handle.Log(shared.LogLevelInfo, "azure-content-safety: unrecognized request format, passing through")
		return shared.BodyStatusContinue
	}
	p := parserForFormat(reqFormat)

	userPrompt, documents, err := p.ParseRequest(bodyBytes)
	if err != nil {
		f.handle.Log(shared.LogLevelInfo,
			"azure-content-safety: failed to parse %s request: %s", reqFormat, err.Error())
		return shared.BodyStatusContinue
	}

	if userPrompt == "" {
		return shared.BodyStatusContinue
	}

	result, err := f.client.ShieldPrompt(userPrompt, documents)
	if err != nil {
		return f.handleAPIError("prompt shield", err)
	}

	if isPromptAttackDetected(result) {
		f.handle.Log(shared.LogLevelInfo, "azure-content-safety: prompt injection attack detected")
		if f.config.isBlockMode() {
			f.handle.SendLocalResponse(
				403,
				nil,
				[]byte("Request blocked: prompt injection detected"),
				"azure_content_safety_prompt_blocked",
			)
			f.metrics.inc(f.handle, decisionBlocked)
			return shared.BodyStatusStopNoBuffer
		}
		f.handle.Log(shared.LogLevelInfo,
			"azure-content-safety: monitor mode - allowing request with detected prompt injection")
		f.metrics.inc(f.handle, decisionMonitored)
		return shared.BodyStatusContinue
	}

	// Task Adherence check (opt-in).
	if f.config.EnableTaskAdherence {
		taReq, err := p.ParseRequestForTaskAdherence(bodyBytes)
		if err != nil {
			f.handle.Log(shared.LogLevelInfo,
				"azure-content-safety: failed to parse %s request for task adherence: %s", reqFormat, err.Error())
			return f.handleAPIError("task adherence parse", err)
		}

		taResult, err := f.client.AnalyzeTaskAdherence(taReq, f.config.taskAdherenceAPIVersion())
		if err != nil {
			return f.handleAPIError("task adherence", err)
		}

		if taResult.TaskRiskDetected {
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
				return shared.BodyStatusStopNoBuffer
			}
			f.handle.Log(shared.LogLevelInfo,
				"azure-content-safety: monitor mode - allowing request with task adherence risk")
			f.metrics.inc(f.handle, decisionMonitored)
			return shared.BodyStatusContinue
		}
	}

	f.metrics.inc(f.handle, decisionAllowed)
	return shared.BodyStatusContinue
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
	body shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}

	bodyBytes := readBodyBuffer(f.handle.BufferedResponseBody(), body)
	if len(bodyBytes) == 0 {
		return shared.BodyStatusContinue
	}

	respFormat := detectResponseFormat(bodyBytes)
	if respFormat == formatUnknown {
		f.handle.Log(shared.LogLevelInfo, "azure-content-safety: unrecognized response format, passing through")
		return shared.BodyStatusContinue
	}

	content, err := parserForFormat(respFormat).ParseResponse(bodyBytes)
	if err != nil {
		f.handle.Log(shared.LogLevelInfo,
			"azure-content-safety: failed to parse %s response: %s", respFormat, err.Error())
		return shared.BodyStatusContinue
	}

	if content == "" {
		return shared.BodyStatusContinue
	}

	result, err := f.client.AnalyzeText(content, f.config.categories())
	if err != nil {
		return f.handleAPIError("text analysis", err)
	}

	violations := f.checkThresholds(result)
	if len(violations) > 0 {
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
			return shared.BodyStatusStopNoBuffer
		}
		f.handle.Log(shared.LogLevelInfo,
			"azure-content-safety: monitor mode - allowing response with detected harmful content")
		f.metrics.inc(f.handle, decisionMonitored)
		return shared.BodyStatusContinue
	}

	// Protected Material check (opt-in).
	if f.config.EnableProtectedMaterial {
		pmResult, err := f.client.DetectProtectedMaterial(content)
		if err != nil {
			return f.handleAPIError("protected material", err)
		}

		if pmResult.ProtectedMaterialAnalysis != nil && pmResult.ProtectedMaterialAnalysis.Detected {
			f.handle.Log(shared.LogLevelInfo, "azure-content-safety: protected material detected")
			if f.config.isBlockMode() {
				f.handle.SendLocalResponse(
					403,
					nil,
					[]byte("Response blocked: protected material detected"),
					"azure_content_safety_protected_material_blocked",
				)
				f.metrics.inc(f.handle, decisionBlocked)
				return shared.BodyStatusStopNoBuffer
			}
			f.handle.Log(shared.LogLevelInfo,
				"azure-content-safety: monitor mode - allowing response with protected material")
			f.metrics.inc(f.handle, decisionMonitored)
			return shared.BodyStatusContinue
		}
	}

	f.metrics.inc(f.handle, decisionAllowed)
	return shared.BodyStatusContinue
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

func (f *contentSafetyFilter) handleAPIError(apiName string, err error) shared.BodyStatus {
	if f.config.FailOpen {
		f.handle.Log(shared.LogLevelWarn,
			"azure-content-safety: %s API error (fail-open): %s", apiName, err.Error())
		f.metrics.inc(f.handle, decisionFailOpen)
		return shared.BodyStatusContinue
	}
	f.handle.Log(shared.LogLevelError,
		"azure-content-safety: %s API error (fail-closed): %s", apiName, err.Error())
	f.handle.SendLocalResponse(
		500, nil,
		[]byte("Internal Server Error"),
		"azure_content_safety_api_error",
	)
	f.metrics.inc(f.handle, decisionError)
	return shared.BodyStatusStopNoBuffer
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

func readBodyBuffer(buffered shared.BodyBuffer, current shared.BodyBuffer) []byte {
	var data []byte
	if buffered != nil {
		for _, chunk := range buffered.GetChunks() {
			data = append(data, chunk...)
		}
	}
	if current != nil {
		for _, chunk := range current.GetChunks() {
			data = append(data, chunk...)
		}
	}
	return data
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
