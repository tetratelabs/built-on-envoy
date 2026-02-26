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

// openfgaMetrics holds metric IDs defined at config time.
type openfgaMetrics struct {
	requestsTotal shared.MetricID
	enabled       bool
}

func (m *openfgaMetrics) inc(handle shared.HttpFilterHandle, decision string) {
	if m.enabled {
		handle.IncrementCounterValue(m.requestsTotal, 1, decision)
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
	rule := f.matchRule(headers)
	if rule == nil {
		f.handle.Log(shared.LogLevelWarn, "openfga: no matching rule for request")
		if f.config.failOpen {
			f.metrics.inc(f.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		f.handle.SendLocalResponse(uint32(f.config.denyStatus), //nolint:gosec
			[][2]string{{"content-type", "text/plain"}},
			[]byte(f.config.denyBody), "openfga_no_rule")
		f.metrics.inc(f.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	user := rule.user.resolve(headers)
	relation := rule.relation.resolve(headers)
	object := rule.object.resolve(headers)

	if user == "" || relation == "" || object == "" {
		f.handle.Log(shared.LogLevelWarn, "openfga: missing check parameters user=%q relation=%q object=%q", user, relation, object)
		if f.config.failOpen {
			f.handle.Log(shared.LogLevelWarn, "openfga: fail_open enabled, allowing request with missing parameters")
			f.metrics.inc(f.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		f.handle.SendLocalResponse(uint32(f.config.denyStatus), //nolint:gosec
			[][2]string{{"content-type", "text/plain"}},
			[]byte(f.config.denyBody), "openfga_missing_params")
		f.metrics.inc(f.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	body := buildCheckBody(user, relation, object, f.config.authorizationModelID)
	f.handle.Log(shared.LogLevelDebug, "openfga: checking user=%s relation=%s object=%s", user, relation, object)

	result, _ := f.handle.HttpCallout(
		f.config.cluster,
		f.config.calloutHeaders,
		body,
		f.config.timeoutMs,
		&openfgaCallback{
			handle:  f.handle,
			config:  f.config,
			metrics: f.metrics,
		},
	)
	if result != shared.HttpCalloutInitSuccess {
		f.handle.Log(shared.LogLevelError, "openfga: failed to initiate callout to cluster %s", f.config.cluster)
		if f.config.failOpen {
			f.metrics.inc(f.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		f.handle.SendLocalResponse(http.StatusBadGateway,
			[][2]string{{"content-type", "text/plain"}},
			[]byte("Authorization service unavailable"), "openfga_callout_failed")
		f.metrics.inc(f.handle, decisionError)
		return shared.HeadersStatusStop
	}

	return shared.HeadersStatusStopAllAndBuffer
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
	handle  shared.HttpFilterHandle
	config  *parsedConfig
	metrics *openfgaMetrics
}

// OnHttpCalloutDone processes the Check API response and continues or denies the request.
func (c *openfgaCallback) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]string, body [][]byte) { //nolint:revive
	fullBody := joinBody(body)

	if result != shared.HttpCalloutSuccess {
		c.handle.Log(shared.LogLevelError, "openfga: callout failed, result=%v", result)
		if c.config.failOpen {
			c.handle.Log(shared.LogLevelWarn, "openfga: fail_open enabled, allowing request after callout failure")
			c.metrics.inc(c.handle, decisionFailOpen)
			c.handle.ContinueRequest()
			return
		}
		c.metrics.inc(c.handle, decisionError)
		c.handle.SendLocalResponse(http.StatusBadGateway,
			[][2]string{{"content-type", "text/plain"}},
			[]byte("Authorization service error"), "openfga_callout_error")
		return
	}

	statusCode := headerValue(headers, ":status")
	if statusCode != httpStatusOKStr {
		c.handle.Log(shared.LogLevelError, "openfga: Check API returned status %s, body=%s", statusCode, fullBody)
		if c.config.failOpen {
			c.handle.Log(shared.LogLevelWarn, "openfga: fail_open enabled, allowing request after API error")
			c.metrics.inc(c.handle, decisionFailOpen)
			c.handle.ContinueRequest()
			return
		}
		c.metrics.inc(c.handle, decisionError)
		c.handle.SendLocalResponse(http.StatusBadGateway,
			[][2]string{{"content-type", "text/plain"}},
			[]byte("Authorization service error"), "openfga_api_error")
		return
	}

	var checkResp struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(fullBody, &checkResp); err != nil {
		c.handle.Log(shared.LogLevelError, "openfga: failed to parse Check response: %s, body=%s", err.Error(), fullBody)
		if c.config.failOpen {
			c.metrics.inc(c.handle, decisionFailOpen)
			c.handle.ContinueRequest()
			return
		}
		c.metrics.inc(c.handle, decisionError)
		c.handle.SendLocalResponse(http.StatusBadGateway,
			[][2]string{{"content-type", "text/plain"}},
			[]byte("Authorization service error"), "openfga_parse_error")
		return
	}

	c.handle.Log(shared.LogLevelDebug, "openfga: Check result allowed=%v", checkResp.Allowed)

	if !checkResp.Allowed && c.config.dryRun {
		c.handle.Log(shared.LogLevelInfo, "openfga: dry_run mode, would have denied request")
		c.metrics.inc(c.handle, decisionDryAllow)
		c.handle.ContinueRequest()
		return
	}

	if !checkResp.Allowed {
		c.handle.Log(shared.LogLevelDebug, "openfga: denying request with status %d", c.config.denyStatus)
		c.metrics.inc(c.handle, decisionDenied)
		c.handle.SendLocalResponse(uint32(c.config.denyStatus), //nolint:gosec
			[][2]string{{"content-type", "text/plain"}},
			[]byte(c.config.denyBody), "openfga_denied")
		return
	}

	c.metrics.inc(c.handle, decisionAllowed)
	c.handle.ContinueRequest()
}

// joinBody concatenates body chunks into a single slice.
func joinBody(body [][]byte) []byte {
	if len(body) == 1 {
		return body[0]
	}
	return bytes.Join(body, nil)
}

// headerValue returns the first value for a key in a callout response header list.
func headerValue(headers [][2]string, key string) string {
	for _, h := range headers {
		if h[0] == key {
			return h[1]
		}
	}
	return ""
}
