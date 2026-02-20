// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.
package oauth2te

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

// oauth2teMetrics holds metric IDs defined at config time, used at request time.
type oauth2teMetrics struct {
	exchanges          shared.MetricID
	hasExchanges       bool
	exchangeResults    shared.MetricID
	hasExchangeResults bool
}

// tokenExchangeFilterFactory creates filter instances with the parsed config.
type tokenExchangeFilterFactory struct {
	config  *tokenExchangeConfig
	metrics *oauth2teMetrics
}

func (f *tokenExchangeFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &tokenExchangeFilter{handle: handle, config: f.config, metrics: f.metrics}
}

// OAuth2TokenExchangeHttpFilterConfigFactory is the configuration factory for the OAuth2 Token Exchange filter.
type OAuth2TokenExchangeHttpFilterConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the configuration and returns a new filter factory.
func (f *OAuth2TokenExchangeHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		handle.Log(shared.LogLevelError, err.Error())
		return nil, err
	}
	handle.Log(shared.LogLevelDebug, "oauth2te: parsed config: %v", cfg)

	// Define metrics.
	metrics := &oauth2teMetrics{}
	if id, status := handle.DefineCounter("oauth2te_token_exchanges_total"); status == shared.MetricsSuccess {
		metrics.exchanges = id
		metrics.hasExchanges = true
	}
	if id, status := handle.DefineCounter("oauth2te_token_exchange_results_total", "result"); status == shared.MetricsSuccess {
		metrics.exchangeResults = id
		metrics.hasExchangeResults = true
	}

	handle.Log(shared.LogLevelInfo, "oauth2te: loaded token exchange config for cluster=%s endpoint=%s",
		cfg.Cluster, cfg.TokenExchangeEndpoint)
	return &tokenExchangeFilterFactory{cfg, metrics}, nil
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{
		"oauth2te": &OAuth2TokenExchangeHttpFilterConfigFactory{},
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

// sendLocalRespError logs the message, sends a local response, and returns HeadersStatusStop.
func sendLocalRespError(handle shared.HttpFilterHandle, level shared.LogLevel, status int, msg string) shared.HeadersStatus {
	handle.Log(level, "oauth2te: %s", msg)
	handle.SendLocalResponse(uint32(status), [][2]string{{"content-type", "text/plain"}}, []byte(msg), "")
	return shared.HeadersStatusStop
}

// tokenExchangeFilter is the HTTP filter that performs OAuth2 Token Exchange.
type tokenExchangeFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	config  *tokenExchangeConfig
	metrics *oauth2teMetrics
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
		return sendLocalRespError(f.handle, shared.LogLevelWarn, http.StatusUnauthorized, "missing Authorization header")
	}
	subjectToken, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return sendLocalRespError(f.handle, shared.LogLevelWarn, http.StatusUnauthorized, "invalid Authorization header")
	}
	if subjectToken == "" {
		return sendLocalRespError(f.handle, shared.LogLevelWarn, http.StatusUnauthorized, "empty bearer token")
	}

	// The static form fields are precomputed at config time; only the subject token is dynamic.
	result, _ := f.handle.HttpCallout(
		f.config.Cluster,
		f.config.calloutHeaders,
		[]byte(f.config.stsPostBodyPrefix+url.QueryEscape(subjectToken)),
		f.config.TimeoutMs,
		&tokenExchangeCallback{handle: f.handle, metrics: f.metrics},
	)
	if result != shared.HttpCalloutInitSuccess {
		return sendLocalRespError(f.handle, shared.LogLevelError, http.StatusBadGateway, "token exchange unavailable")
	}

	f.incrementExchanges()
	f.handle.Log(shared.LogLevelInfo, "oauth2te: token exchange callout initiated")
	return shared.HeadersStatusStopAllAndBuffer
}

// tokenExchangeCallback handles the STS endpoint response.
type tokenExchangeCallback struct {
	handle  shared.HttpFilterHandle
	metrics *oauth2teMetrics
}

// OnHttpCalloutDone is called when the STS response is received. It processes the response and either continues
// the request with the new token replacing the original one or sends a local error response.
func (c *tokenExchangeCallback) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]string, body [][]byte) {
	if result != shared.HttpCalloutSuccess {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "token exchange failed")
		return
	}

	// Check the HTTP status from the STS response.
	statusCode := headerValue(headers, ":status")
	if len(statusCode) == 0 {
		c.handle.Log(shared.LogLevelError, "oauth2te: STS response missing :status header")
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "token exchange failed: invalid response")
		return
	}
	// For 4xx errors, we can attempt to parse error response for better logging.
	if statusCode[0] == '4' {
		var stsErr struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if err := json.Unmarshal(joinBody(body), &stsErr); err == nil && stsErr.Error != "" {
			c.handle.Log(shared.LogLevelError, "oauth2te: STS returned %s: error=%s description=%s",
				statusCode, stsErr.Error, stsErr.Description)
		} else {
			c.handle.Log(shared.LogLevelError, "oauth2te: STS endpoint returned status %s", statusCode)
		}
		c.incrementExchangeResult(metricResRejected)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusUnauthorized, "token exchange rejected")
		return
	}
	if statusCode != httpStatusOKStr {
		c.handle.Log(shared.LogLevelError, "oauth2te: STS endpoint returned status %s", statusCode)
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "token exchange error")
		return
	}

	// Parse the JSON token response.
	fullBody := joinBody(body)

	var tokenResp struct {
		AccessToken     string `json:"access_token"`
		TokenType       string `json:"token_type"`
		IssuedTokenType string `json:"issued_token_type"`
	}

	if err := json.Unmarshal(fullBody, &tokenResp); err != nil {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: "+err.Error())
		return
	}
	if tokenResp.AccessToken == "" {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: missing required access_token")
		return
	}
	if tokenResp.TokenType == "" {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: missing required token_type")
		return
	}
	if tokenResp.IssuedTokenType == "" {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: missing required issued_token_type")
		return
	}
	if strings.EqualFold(tokenResp.TokenType, tokenTypeNotApplicable) {
		c.incrementExchangeResult(metricResError)
		sendLocalRespError(c.handle, shared.LogLevelError, http.StatusBadGateway, "invalid token exchange response: returned token_type N_A")
		return
	}

	// Replace the Authorization header with the exchanged token.
	reqHeaders := c.handle.RequestHeaders()
	reqHeaders.Set("authorization", fmt.Sprintf("%s %s", tokenResp.TokenType, tokenResp.AccessToken))
	c.incrementExchangeResult(metricResSuccess)
	c.handle.Log(shared.LogLevelInfo, "oauth2te: token exchange succeeded, token_type=%s issued_token_type=%s", tokenResp.TokenType, tokenResp.IssuedTokenType)
	c.handle.ContinueRequest()
}
