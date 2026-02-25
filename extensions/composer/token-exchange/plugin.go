// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package oauth2te implements an OAuth2 Token Exchange (RFC 8693) filter for Envoy.
package oauth2te

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

const (
	httpStatusOKStr        = "200"
	tokenTypeNotApplicable = "N_A"

	metricResSuccess  = "success"
	metricResRejected = "rejected"
	metricResError    = "error"
)

// tokenExchangeMetrics holds metric IDs defined at config time, used at request time.
type tokenExchangeMetrics struct {
	exchanges          shared.MetricID
	hasExchanges       bool
	exchangeResults    shared.MetricID
	hasExchangeResults bool
}

// tokenExchangeFilterFactory creates filter instances with the parsed config.
type tokenExchangeFilterFactory struct {
	shared.EmptyHttpFilterFactory
	config  *tokenExchangeConfig
	metrics *tokenExchangeMetrics
}

func (f *tokenExchangeFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &tokenExchangeFilter{handle: handle, config: f.config, metrics: f.metrics}
}

// tokenExchangeHttpFilterConfigFactory is the configuration factory for the OAuth2 Token Exchange filter.
type tokenExchangeHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the configuration and returns a new filter factory.
func (f *tokenExchangeHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		handle.Log(shared.LogLevelError, err.Error())
		return nil, err
	}
	handle.Log(shared.LogLevelDebug, "token-exchange: parsed config: %v", cfg)

	// Define metrics.
	metrics := &tokenExchangeMetrics{}
	if id, status := handle.DefineCounter("token_exchange_requests_total"); status == shared.MetricsSuccess {
		metrics.exchanges = id
		metrics.hasExchanges = true
	}
	if id, status := handle.DefineCounter("token_exchange_results_total", "result"); status == shared.MetricsSuccess {
		metrics.exchangeResults = id
		metrics.hasExchangeResults = true
	}

	handle.Log(shared.LogLevelInfo, "token-exchange: loaded token exchange config for cluster=%s url=%s",
		cfg.Cluster, cfg.TokenExchangeURL)
	return &tokenExchangeFilterFactory{config: cfg, metrics: metrics}, nil
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		"token-exchange": &tokenExchangeHttpFilterConfigFactory{},
	}
}

// joinBody returns the body as a single byte slice, avoiding to call bytes.Join
// when there is only one chunk which always returns a copy.
func joinBody(body [][]byte) []byte {
	if len(body) == 1 {
		return body[0]
	}
	return bytes.Join(body, nil)
}

// headerValue returns the first value for a key in a [][2]string header list.
func headerValue(headers [][2]string, key string) string {
	for _, h := range headers {
		if h[0] == key {
			return h[1]
		}
	}
	return ""
}

// sendLocalRespError logs the message (with optional raw body), sends
// a local response with the message, and returns HeadersStatusStop.
func sendLocalRespError(handle shared.HttpFilterHandle, level shared.LogLevel, status uint32, msg string, rawBody []byte) shared.HeadersStatus {
	if len(rawBody) > 0 {
		handle.Log(level, "token-exchange: %s, raw_body=%s", msg, rawBody)
	} else {
		handle.Log(level, "token-exchange: %s", msg)
	}
	handle.SendLocalResponse(status, [][2]string{{"content-type", "text/plain"}}, []byte(msg), "")
	return shared.HeadersStatusStop
}

// tokenExchangeFilter is the HTTP filter that performs OAuth2 Token Exchange.
type tokenExchangeFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	config  *tokenExchangeConfig
	metrics *tokenExchangeMetrics
}

// incrementExchanges increments the token exchanges counter.
func (f *tokenExchangeFilter) incrementExchanges() {
	if m := f.metrics; m != nil && m.hasExchanges {
		f.handle.IncrementCounterValue(m.exchanges, 1)
	}
}

// incrementExchangeResult increments the token exchange results counter with a result tag.
func (c *tokenExchangeCallback) incrementExchangeResult(result string) {
	if m := c.metrics; m != nil && m.hasExchangeResults {
		c.handle.IncrementCounterValue(m.exchangeResults, 1, result)
	}
}

func (f *tokenExchangeFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	// Extract the bearer token from the Authorization header.
	authHeader := headers.GetOne("authorization")
	if authHeader == "" {
		return sendLocalRespError(f.handle, shared.LogLevelWarn, http.StatusUnauthorized, "missing Authorization header", nil)
	}
	subjectToken, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return sendLocalRespError(f.handle, shared.LogLevelWarn, http.StatusUnauthorized, "invalid Authorization header", nil)
	}
	if subjectToken == "" {
		return sendLocalRespError(f.handle, shared.LogLevelWarn, http.StatusUnauthorized, "empty bearer token", nil)
	}

	// The static form fields are precomputed at config time; only the subject token is dynamic.
	result, _ := f.handle.HttpCallout(
		f.config.Cluster,
		f.config.calloutHeaders,
		f.config.stsPostBody(subjectToken),
		f.config.TimeoutMs,
		&tokenExchangeCallback{handle: f.handle, metrics: f.metrics},
	)
	if result != shared.HttpCalloutInitSuccess {
		return sendLocalRespError(f.handle, shared.LogLevelError, http.StatusBadGateway, "token exchange unavailable", nil)
	}

	f.incrementExchanges()
	f.handle.Log(shared.LogLevelInfo, "token-exchange: token exchange callout initiated")
	return shared.HeadersStatusStopAllAndBuffer
}

// tokenExchangeCallback handles the STS endpoint response.
type tokenExchangeCallback struct {
	handle  shared.HttpFilterHandle
	metrics *tokenExchangeMetrics
}

// OnHttpCalloutDone is called when the STS response is received. It processes the response and either continues
// the request with the new token replacing the original one or sends a local error response.
func (c *tokenExchangeCallback) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]string, body [][]byte) { //nolint:revive
	fullBody := joinBody(body)

	if result != shared.HttpCalloutSuccess {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, fmt.Sprintf("callout failed, result=%v", result), fullBody)
		return
	}

	// Check the HTTP status from the STS response.
	statusCode := headerValue(headers, ":status")
	if len(statusCode) == 0 {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "token exchange failed: invalid response", fullBody)
		return
	}

	// For 4xx errors, we can attempt to parse error response for better logging.
	if statusCode[0] == '4' {
		var stsErr struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if err := json.Unmarshal(fullBody, &stsErr); err == nil && stsErr.Error != "" {
			c.handle.Log(shared.LogLevelError, "token-exchange: STS returned %s: error=%s description=%s",
				statusCode, stsErr.Error, stsErr.Description)
		}
		c.incrementExchangeResult(metricResRejected)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusUnauthorized, fmt.Sprintf("token exchange rejected: STS returned %s", statusCode), fullBody)
		return
	}
	if statusCode != httpStatusOKStr {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, fmt.Sprintf("token exchange error: STS returned %s", statusCode), fullBody)
		return
	}

	// Parse the JSON token response.
	var tokenResp struct {
		AccessToken     string `json:"access_token"`
		TokenType       string `json:"token_type"`
		IssuedTokenType string `json:"issued_token_type"`
	}

	if err := json.Unmarshal(fullBody, &tokenResp); err != nil {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: "+err.Error(), fullBody)
		return
	}
	if tokenResp.AccessToken == "" {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: missing required access_token", fullBody)
		return
	}
	if tokenResp.TokenType == "" {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: missing required token_type", fullBody)
		return
	}
	if tokenResp.IssuedTokenType == "" {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: missing required issued_token_type", fullBody)
		return
	}
	if strings.EqualFold(tokenResp.TokenType, tokenTypeNotApplicable) {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: returned token_type N_A", fullBody)
		return
	}

	// Replace the Authorization header with the exchanged token.
	reqHeaders := c.handle.RequestHeaders()
	reqHeaders.Set("authorization", fmt.Sprintf("%s %s", tokenResp.TokenType, tokenResp.AccessToken))
	c.incrementExchangeResult(metricResSuccess)
	c.handle.Log(shared.LogLevelInfo, "token-exchange: token exchange succeeded, token_type=%s issued_token_type=%s", tokenResp.TokenType, tokenResp.IssuedTokenType)
	c.handle.ContinueRequest()
}
