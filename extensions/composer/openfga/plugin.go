// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package openfga implements an OpenFGA authorization HTTP filter plugin.
package openfga

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

const (
	decisionAllowed  = "allowed"
	decisionDenied   = "denied"
	decisionFailOpen = "failopen"
	decisionDryAllow = "dryrun_allow"
	decisionError    = "error"

	httpStatusOKStr = "200"
)

var (
	errorResponseHeaders = [][2]string{{"content-type", "text/plain"}}
	errorBodyCallout     = []byte("Authorization service unavailable")
	errorBodyAPI         = []byte("Authorization service error")
)

// openfgaMetrics holds metric IDs defined at config time.
type openfgaMetrics struct {
	requestsTotal shared.MetricID
	checkDuration shared.MetricID
	enabled       bool
	hasCheckDur   bool
}

func (m *openfgaMetrics) inc(handle shared.HttpFilterHandle, decision string) {
	if m.enabled {
		handle.IncrementCounterValue(m.requestsTotal, 1, decision)
	}
}

func (m *openfgaMetrics) recordDuration(handle shared.HttpFilterHandle, ms uint64) {
	if m.hasCheckDur {
		handle.RecordHistogramValue(m.checkDuration, ms)
	}
}

// openfgaFilterFactory creates per-request filter instances.
type openfgaFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config  *parsedConfig
	metrics *openfgaMetrics
}

func (f *openfgaFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &openfgaFilter{handle: handle, config: f.config, metrics: f.metrics}
}

// OpenFGAHttpFilterConfigFactory is the configuration factory for this filter.
type OpenFGAHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON configuration and creates a filter factory.
func (f *OpenFGAHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		handle.Log(shared.LogLevelError, err.Error())
		return nil, err
	}

	metrics := &openfgaMetrics{}
	if id, status := handle.DefineCounter("openfga_requests_total", "decision"); status == shared.MetricsSuccess {
		metrics.requestsTotal = id
		metrics.enabled = true
	}
	if id, status := handle.DefineHistogram("openfga_check_duration_ms"); status == shared.MetricsSuccess {
		metrics.checkDuration = id
		metrics.hasCheckDur = true
	}

	handle.Log(shared.LogLevelInfo, "openfga: loaded config cluster=%s store=%s rules=%d",
		cfg.cluster, cfg.checkPath, len(cfg.rules))
	return &openfgaFilterFactory{config: cfg, metrics: metrics}, nil
}

// WellKnownHttpFilterConfigFactories registers the extension.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		"openfga": &OpenFGAHttpFilterConfigFactory{},
	}
}

// openfgaFilter is the per-request HTTP filter.
type openfgaFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	config  *parsedConfig
	metrics *openfgaMetrics
}

func (f *openfgaFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	method := headers.GetOne(":method").ToString()
	path := headers.GetOne(":path").ToString()

	rule := f.matchRule(headers)
	if rule == nil {
		return f.handleDeny(shared.LogLevelWarn, "openfga: no matching rule for %s %s", "openfga_no_rule", decisionDenied, method, path)
	}

	user := rule.user.resolve(headers)
	relation := rule.relation.resolve(headers)
	object := rule.object.resolve(headers)

	if user == "" || relation == "" || object == "" {
		f.handle.Log(shared.LogLevelWarn, "openfga: missing check parameters for %s %s user=%q relation=%q object=%q", method, path, user, relation, object)
		if f.config.failOpen {
			f.handle.Log(shared.LogLevelWarn, "openfga: fail_open enabled, allowing request with missing parameters")
			f.config.writeMetadata(f.handle, decisionFailOpen)
			f.metrics.inc(f.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		f.config.writeMetadata(f.handle, decisionDenied)
		f.config.sendDeny(f.handle, "openfga_missing_params")
		f.metrics.inc(f.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	body := buildCheckBody(user, relation, object, f.config.authorizationModelID)
	f.handle.Log(shared.LogLevelDebug, "openfga: checking %s %s user=%s relation=%s object=%s", method, path, user, relation, object)

	result, _ := f.handle.HttpCallout(
		f.config.cluster,
		f.config.calloutHeaders,
		body,
		f.config.timeoutMs,
		&openfgaCallback{
			handle:    f.handle,
			config:    f.config,
			metrics:   f.metrics,
			startTime: time.Now(),
		},
	)
	if result != shared.HttpCalloutInitSuccess {
		return f.handleCalloutError("openfga: failed to initiate callout to cluster %s", f.config.cluster)
	}

	return shared.HeadersStatusStopAllAndBuffer
}

// handleDeny handles a deny case: log, then fail-open or send deny response.
func (f *openfgaFilter) handleDeny(logLevel shared.LogLevel, logFormat, grpcStatus, metricDecision string, logArgs ...any) shared.HeadersStatus {
	f.handle.Log(logLevel, logFormat, logArgs...)
	if f.config.failOpen {
		f.config.writeMetadata(f.handle, decisionFailOpen)
		f.metrics.inc(f.handle, decisionFailOpen)
		return shared.HeadersStatusContinue
	}
	f.config.writeMetadata(f.handle, metricDecision)
	f.config.sendDeny(f.handle, grpcStatus)
	f.metrics.inc(f.handle, metricDecision)
	return shared.HeadersStatusStop
}

// handleCalloutError handles callout init failure: log, then fail-open or send 502.
func (f *openfgaFilter) handleCalloutError(logMsg string, args ...any) shared.HeadersStatus {
	f.handle.Log(shared.LogLevelError, logMsg, args...)
	if f.config.failOpen {
		f.config.writeMetadata(f.handle, decisionFailOpen)
		f.metrics.inc(f.handle, decisionFailOpen)
		return shared.HeadersStatusContinue
	}
	f.config.writeMetadata(f.handle, decisionError)
	f.handle.SendLocalResponse(http.StatusBadGateway, errorResponseHeaders, errorBodyCallout, "openfga_callout_failed")
	f.metrics.inc(f.handle, decisionError)
	return shared.HeadersStatusStop
}

func (f *openfgaFilter) matchRule(headers shared.HeaderMap) *parsedRule {
	for i := range f.config.rules {
		r := &f.config.rules[i]
		if r.match == nil || r.match.matches(headers) {
			return r
		}
	}
	return nil
}

// openfgaCallback handles the OpenFGA Check API response.
type openfgaCallback struct {
	handle    shared.HttpFilterHandle
	config    *parsedConfig
	metrics   *openfgaMetrics
	startTime time.Time
}

// handleCallbackError handles callout/API/parse errors: log, then fail-open or send 502.
func (c *openfgaCallback) handleCallbackError(logFormat, grpcStatus string, responseBody []byte, logArgs ...any) {
	c.handle.Log(shared.LogLevelError, logFormat, logArgs...)
	if c.config.failOpen {
		c.handle.Log(shared.LogLevelWarn, "openfga: fail_open enabled, allowing request after error")
		c.config.writeMetadata(c.handle, decisionFailOpen)
		c.metrics.inc(c.handle, decisionFailOpen)
		c.handle.ContinueRequest()
		return
	}
	c.config.writeMetadata(c.handle, decisionError)
	c.metrics.inc(c.handle, decisionError)
	c.handle.SendLocalResponse(http.StatusBadGateway, errorResponseHeaders, responseBody, grpcStatus)
}

// OnHttpCalloutDone processes the Check API response and continues or denies the request.
func (c *openfgaCallback) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]shared.UnsafeEnvoyBuffer, body []shared.UnsafeEnvoyBuffer) { //nolint:revive
	elapsed := time.Since(c.startTime).Milliseconds()
	c.metrics.recordDuration(c.handle, uint64(elapsed))

	fullBody := joinBody(body)

	if result != shared.HttpCalloutSuccess {
		c.handleCallbackError("openfga: callout failed, result=%v", "openfga_callout_error", errorBodyAPI, result)
		return
	}

	statusCode := headerValue(headers, ":status")
	if statusCode != httpStatusOKStr {
		c.handleCallbackError("openfga: Check API returned status %s, body=%s", "openfga_api_error", errorBodyAPI, statusCode, fullBody)
		return
	}

	if len(fullBody) == 0 {
		c.handleCallbackError("openfga: Check API returned empty body", "openfga_empty_body", errorBodyAPI)
		return
	}

	var checkResp struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(fullBody, &checkResp); err != nil {
		c.handleCallbackError("openfga: failed to parse Check response: %s, body=%s", "openfga_parse_error", errorBodyAPI, err.Error(), fullBody)
		return
	}

	c.handle.Log(shared.LogLevelDebug, "openfga: Check result allowed=%v", checkResp.Allowed)

	if !checkResp.Allowed && c.config.dryRun {
		c.handle.Log(shared.LogLevelInfo, "openfga: dry_run mode, would have denied request")
		c.config.writeMetadata(c.handle, decisionDryAllow)
		c.metrics.inc(c.handle, decisionDryAllow)
		c.handle.ContinueRequest()
		return
	}

	if !checkResp.Allowed {
		c.handle.Log(shared.LogLevelDebug, "openfga: denying request with status %d", c.config.deny.Status)
		c.config.writeMetadata(c.handle, decisionDenied)
		c.metrics.inc(c.handle, decisionDenied)
		c.config.sendDeny(c.handle, "openfga_denied")
		return
	}

	c.config.writeMetadata(c.handle, decisionAllowed)
	c.metrics.inc(c.handle, decisionAllowed)
	c.handle.ContinueRequest()
}

// joinBody concatenates body chunks into a single slice.
func joinBody(body []shared.UnsafeEnvoyBuffer) []byte {
	if len(body) == 0 {
		return nil
	}
	if len(body) == 1 {
		return body[0].ToBytes()
	}
	chunks := make([][]byte, len(body))
	for i, b := range body {
		chunks[i] = b.ToBytes()
	}
	return bytes.Join(chunks, nil)
}

// headerValue returns the first value for a key in a callout response header list.
func headerValue(headers [][2]shared.UnsafeEnvoyBuffer, key string) string {
	for _, h := range headers {
		if h[0].ToString() == key {
			return h[1].ToString()
		}
	}
	return ""
}
