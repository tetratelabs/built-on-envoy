// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package openfga implements an OpenFGA authorization HTTP filter plugin.
package openfga

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
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

func (m *openfgaMetrics) recordDuration(handle shared.HttpFilterHandle, d time.Duration) {
	if m.hasCheckDur {
		ms := uint64(max(0, d.Milliseconds())) //nolint:gosec
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
	cfg := f.config
	if perRoute := pkg.GetMostSpecificConfig[*parsedConfig](handle); perRoute != nil {
		cfg = perRoute
	}
	if cfg == nil {
		handle.Log(shared.LogLevelInfo, "openfga: no config available, using empty filter")
		return &shared.EmptyHttpFilter{}
	}
	return &openfgaFilter{handle: handle, config: cfg, metrics: f.metrics}
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

	if cfg.authorizationModelID == "" {
		handle.Log(shared.LogLevelWarn, "openfga: authorization_model_id not set; using latest model — set explicitly for production use")
	}
	if w := warnStoreIDFormat(cfg.storeID); w != "" {
		handle.Log(shared.LogLevelWarn, w)
	}

	handle.Log(shared.LogLevelInfo, "openfga: loaded config cluster=%s store_id=%s rules=%d check_path=%s",
		cfg.cluster, cfg.storeID, len(cfg.rules), cfg.checkPath)
	return &openfgaFilterFactory{config: cfg, metrics: metrics}, nil
}

// CreatePerRoute parses per-route configuration for the openfga filter.
func (f *OpenFGAHttpFilterConfigFactory) CreatePerRoute(unparsedConfig []byte) (any, error) {
	cfg, err := parseConfig(unparsedConfig)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("openfga: per-route config is empty or invalid")
	}
	return cfg, nil
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
	method := headers.GetOne(":method").ToUnsafeString()
	path := headers.GetOne(":path").ToUnsafeString()

	rule := f.matchRule(headers)
	if rule == nil {
		return f.handleDeny(shared.LogLevelWarn, "openfga: no matching rule for %s %s", "openfga_no_rule", decisionDenied, method, path)
	}

	user := rule.user.resolve(headers)
	relation := rule.relation.resolve(headers)
	object := rule.object.resolve(headers)

	if user == "" || relation == "" || object == "" {
		return f.handleDeny(shared.LogLevelWarn,
			"openfga: missing check parameters for %s %s user=%q relation=%q object=%q",
			"openfga_missing_params", decisionDenied,
			method, path, user, relation, object)
	}

	// Resolve contextual tuples from request headers.
	var ctxTuples []resolvedTuple
	for i := range f.config.contextualTuples {
		ct := &f.config.contextualTuples[i]
		u := ct.user.resolve(headers)
		r := ct.relation.resolve(headers)
		o := ct.object.resolve(headers)
		if u == "" || r == "" || o == "" {
			f.handle.Log(shared.LogLevelDebug, "openfga: skipping contextual_tuple[%d]: incomplete (user=%q relation=%q object=%q)", i, u, r, o)
			continue
		}
		ctxTuples = append(ctxTuples, resolvedTuple{User: u, Relation: r, Object: o})
	}

	// Resolve ABAC context values from request headers.
	var ctxMap map[string]string
	if len(f.config.context) > 0 {
		ctxMap = make(map[string]string, len(f.config.context))
		for name, vs := range f.config.context {
			vsCopy := vs
			v := vsCopy.resolve(headers)
			if v != "" {
				ctxMap[name] = v
			}
		}
		if len(ctxMap) == 0 {
			ctxMap = nil
		}
	}

	body, err := buildCheckBody(user, relation, object, f.config.authorizationModelID, f.config.consistency, ctxTuples, ctxMap)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "openfga: %v", err)
		return f.handleCalloutError("openfga: failed to build Check request body")
	}
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

// handleDeny handles a deny case: log, then dry-run allow, fail-open, or send deny response.
func (f *openfgaFilter) handleDeny(logLevel shared.LogLevel, logFormat, detail, metricDecision string, logArgs ...any) shared.HeadersStatus {
	f.handle.Log(logLevel, logFormat, logArgs...)
	if f.config.dryRun {
		f.handle.Log(shared.LogLevelInfo, "openfga: dry_run mode, would have denied: %s", detail)
		writeMetadata(f.handle, f.config, decisionDryAllow)
		f.metrics.inc(f.handle, decisionDryAllow)
		return shared.HeadersStatusContinue
	}
	if f.config.failOpen {
		writeMetadata(f.handle, f.config, decisionFailOpen)
		f.metrics.inc(f.handle, decisionFailOpen)
		return shared.HeadersStatusContinue
	}
	writeMetadata(f.handle, f.config, metricDecision)
	sendDeny(f.handle, f.config, detail)
	f.metrics.inc(f.handle, metricDecision)
	return shared.HeadersStatusStop
}

// handleCalloutError handles callout init failure: log, then dry-run allow, fail-open, or send 502.
func (f *openfgaFilter) handleCalloutError(logMsg string, args ...any) shared.HeadersStatus {
	f.handle.Log(shared.LogLevelError, logMsg, args...)
	if f.config.dryRun {
		f.handle.Log(shared.LogLevelInfo, "openfga: dry_run mode, would have errored: openfga_callout_failed")
		writeMetadata(f.handle, f.config, decisionDryAllow)
		f.metrics.inc(f.handle, decisionDryAllow)
		return shared.HeadersStatusContinue
	}
	if f.config.failOpen {
		writeMetadata(f.handle, f.config, decisionFailOpen)
		f.metrics.inc(f.handle, decisionFailOpen)
		return shared.HeadersStatusContinue
	}
	writeMetadata(f.handle, f.config, decisionError)
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

// handleCallbackError handles callout/API/parse errors: log, then dry-run allow, fail-open, or send 502.
func (c *openfgaCallback) handleCallbackError(logFormat, detail string, responseBody []byte, logArgs ...any) {
	c.handle.Log(shared.LogLevelError, logFormat, logArgs...)
	if c.config.dryRun {
		c.handle.Log(shared.LogLevelInfo, "openfga: dry_run mode, would have errored: %s", detail)
		writeMetadata(c.handle, c.config, decisionDryAllow)
		c.metrics.inc(c.handle, decisionDryAllow)
		c.handle.ContinueRequest()
		return
	}
	if c.config.failOpen {
		c.handle.Log(shared.LogLevelWarn, "openfga: fail_open enabled, allowing request after error")
		writeMetadata(c.handle, c.config, decisionFailOpen)
		c.metrics.inc(c.handle, decisionFailOpen)
		c.handle.ContinueRequest()
		return
	}
	writeMetadata(c.handle, c.config, decisionError)
	c.metrics.inc(c.handle, decisionError)
	c.handle.SendLocalResponse(http.StatusBadGateway, errorResponseHeaders, responseBody, detail)
}

// OnHttpCalloutDone processes the Check API response and continues or denies the request.
func (c *openfgaCallback) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]shared.UnsafeEnvoyBuffer, body []shared.UnsafeEnvoyBuffer) { //nolint:revive
	c.metrics.recordDuration(c.handle, time.Since(c.startTime))

	fullBody := joinCalloutBody(body)

	if result != shared.HttpCalloutSuccess {
		c.handleCallbackError("openfga: callout failed, result=%v", "openfga_callout_error", errorBodyAPI, result)
		return
	}

	statusCode := calloutHeaderValue(headers, ":status")
	if statusCode != httpStatusOKStr {
		// Attempt to parse OpenFGA structured error for better diagnostics.
		var errResp struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(fullBody, &errResp) == nil && errResp.Message != "" {
			c.handle.Log(shared.LogLevelError, "openfga: Check API error: status=%s code=%s message=%s", statusCode, errResp.Code, errResp.Message)
		}
		if statusCode == "400" {
			c.handle.Log(shared.LogLevelError, "openfga: 400 Bad Request usually indicates a misconfigured tuple or invalid authorization_model_id")
		}
		c.handleCallbackError("openfga: Check API returned status %s", "openfga_api_error", errorBodyAPI, statusCode)
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
		writeMetadata(c.handle, c.config, decisionDryAllow)
		c.metrics.inc(c.handle, decisionDryAllow)
		c.handle.ContinueRequest()
		return
	}

	if !checkResp.Allowed {
		c.handle.Log(shared.LogLevelDebug, "openfga: denying request with status %d", c.config.deny.Status)
		writeMetadata(c.handle, c.config, decisionDenied)
		c.metrics.inc(c.handle, decisionDenied)
		sendDeny(c.handle, c.config, "openfga_denied")
		return
	}

	c.handle.Log(shared.LogLevelDebug, "openfga: allowing request")
	writeMetadata(c.handle, c.config, decisionAllowed)
	c.metrics.inc(c.handle, decisionAllowed)
	c.handle.ContinueRequest()
}

// headerValue returns the first value for a key in an outbound HttpCallout request header list.
func headerValue(headers [][2]string, key string) string {
	for _, h := range headers {
		if h[0] == key {
			return h[1]
		}
	}
	return ""
}

// sendDeny sends a local response using the configured deny status, body, and headers.
func sendDeny(handle shared.HttpFilterHandle, cfg *parsedConfig, detail string) {
	// Status is validated to 100-599 in parseConfig.
	handle.SendLocalResponse(uint32(cfg.deny.Status), cfg.denyHeaders, cfg.denyBodyBytes, detail) //nolint:gosec
}

// writeMetadata writes the authorization decision to dynamic metadata if configured.
func writeMetadata(handle shared.HttpFilterHandle, cfg *parsedConfig, decision string) {
	if cfg.metadata != nil {
		handle.SetMetadata(cfg.metadata.Namespace, cfg.metadata.Key, decision)
	}
}

// joinCalloutBody concatenates the body chunks from an HTTP callout response
// into a single byte slice. Returns nil for an empty body.
func joinCalloutBody(body []shared.UnsafeEnvoyBuffer) []byte {
	if len(body) == 0 {
		return nil
	}
	if len(body) == 1 {
		return body[0].ToUnsafeBytes()
	}
	buffers := make([][]byte, len(body))
	for i, b := range body {
		buffers[i] = b.ToUnsafeBytes()
	}
	return bytes.Join(buffers, nil)
}

// calloutHeaderValue returns the first value for a key in an HTTP callout
// response header list.
func calloutHeaderValue(headers [][2]shared.UnsafeEnvoyBuffer, key string) string {
	for _, h := range headers {
		if h[0].ToUnsafeString() == key {
			return h[1].ToUnsafeString()
		}
	}
	return ""
}
