// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package opa implements an OPA authorization HTTP filter plugin.
package opa

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/open-policy-agent/opa/v1/rego"
)

// defaultDecisionPath is the default OPA rule path to query if not specified in config.
const defaultDecisionPath = "envoy.authz.allow"

// opaConfig represents the JSON configuration for this filter.
type opaConfig struct {
	// PolicyFiles is the paths to the .rego policy files.
	PolicyFiles []string `json:"policy_files"`
	// InlinePolicies provides the policies directly in the config as strings.
	// This takes precedence over PolicyFiles if both are provided.
	InlinePolicies []string `json:"inline_policies"`
	// DecisionPath is the OPA rule path to query (default: "envoy.authz.allow").
	DecisionPath string `json:"decision_path"`
	// FailOpen allows requests if there is an error evaluating the policy.
	// If false, errors will result in a 500 response.
	FailOpen bool `json:"fail_open"`
	// DryRun when true logs the decision but always allows the request.
	DryRun bool `json:"dry_run"`
}

// Metric tag values for authorization decisions.
const (
	decisionAllowed  = "allowed"
	decisionDenied   = "denied"
	decisionFailOpen = "failopen"
	decisionDryAllow = "dryrun_allow"
)

// opaMetrics holds the metric IDs for the OPA filter.
type opaMetrics struct {
	requestsTotal shared.MetricID
	enabled       bool
}

// IncRequestsTotal increments the requests counter with the given decision tag value.
func (m opaMetrics) IncRequestsTotal(handle shared.HttpFilterHandle, decision string) {
	if m.enabled {
		handle.IncrementCounterValue(m.requestsTotal, 1, decision)
	}
}

// opaParsedConfig holds the parsed configuration and compiled OPA query.
type opaParsedConfig struct {
	opaConfig
	preparedQuery rego.PreparedEvalQuery
	metrics       opaMetrics
}

// opaHttpFilter is the per-request HTTP filter instance.
type opaHttpFilter struct { //nolint:revive
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *opaParsedConfig
}

// policyResponse holds optional structured response details from the policy.
type policyResponse struct {
	httpStatus int
	headers    map[string]string
	body       string
}

func (o *opaHttpFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	input := o.buildInput(headers)
	o.handle.Log(shared.LogLevelDebug, "opa: evaluating policy for %s %s",
		headers.GetOne(":method"), headers.GetOne(":path"))

	rs, err := o.config.preparedQuery.Eval(
		context.Background(),
		rego.EvalInput(input),
	)
	if err != nil {
		if o.config.FailOpen {
			o.handle.Log(shared.LogLevelError, "opa: policy evaluation error (fail_open enabled): %s", err.Error())
			o.config.metrics.IncRequestsTotal(o.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		o.handle.Log(shared.LogLevelError, "opa: policy evaluation error: %s", err.Error())
		o.handle.SendLocalResponse(500, nil, []byte("Internal Server Error"), "opa_eval_error")
		o.config.metrics.IncRequestsTotal(o.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	allowed, resp := interpretResult(rs)
	o.handle.Log(shared.LogLevelDebug, "opa: decision: allowed=%v", allowed)

	if !allowed && o.config.DryRun {
		o.handle.Log(shared.LogLevelInfo, "opa: dry-run decision: allowed=%v", allowed)
		o.config.metrics.IncRequestsTotal(o.handle, decisionDryAllow)
		allowed = true
	}

	if !allowed {
		status := resp.httpStatus
		if status == 0 {
			status = 403
		}
		var responseHeaders [][2]string
		for k, v := range resp.headers {
			responseHeaders = append(responseHeaders, [2]string{k, v})
		}
		body := resp.body
		if body == "" {
			body = "Forbidden"
		}
		o.handle.Log(shared.LogLevelDebug, "opa: denying request with status %d", status)
		o.handle.SendLocalResponse(
			uint32(status), //nolint:gosec
			responseHeaders,
			[]byte(body),
			"opa_denied",
		)
		o.config.metrics.IncRequestsTotal(o.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	// If allowed and policy returned headers, add them to the request.
	for k, v := range resp.headers {
		o.handle.Log(shared.LogLevelDebug, "opa: adding header %s=%s", k, v)
		o.handle.RequestHeaders().Set(k, v)
	}

	if !o.config.DryRun {
		o.config.metrics.IncRequestsTotal(o.handle, decisionAllowed)
	}

	return shared.HeadersStatusContinue
}

// buildInput constructs the input document for OPA evaluation based on request headers and attributes.
func (o *opaHttpFilter) buildInput(headers shared.HeaderMap) map[string]any {
	var (
		method = headers.GetOne(":method")
		path   = headers.GetOne(":path")
		host   = headers.GetOne(":authority")
		scheme = cmp.Or(headers.GetOne(":scheme"), "http")
	)
	parsedPath, parsedQuery := parsePath(path)
	protocol, _ := o.handle.GetAttributeString(shared.AttributeIDRequestProtocol)
	protocol = cmp.Or(protocol, "HTTP/1.1")

	// Build headers map excluding pseudo-headers.
	headerMap := make(map[string]string)
	for _, h := range headers.GetAll() {
		key := h[0]
		val := h[1]
		if !strings.HasPrefix(key, ":") {
			headerMap[key] = val
		}
	}

	var (
		sourceAddr, _ = o.handle.GetAttributeString(shared.AttributeIDSourceAddress)
		destAddr, _   = o.handle.GetAttributeString(shared.AttributeIDDestinationAddress)
		// Extract connection/TLS attributes for mTLS-aware policies.
		uriSanPeer, _   = o.handle.GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate)
		dnsSanPeer, _   = o.handle.GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate)
		subjectPeer, _  = o.handle.GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate)
		tlsVersion, _   = o.handle.GetAttributeString(shared.AttributeIDConnectionTlsVersion)
		sha256Digest, _ = o.handle.GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest)
		// TODO(nacx): The ABI does not expose a method to get Boolean attributes
		// mtls, _         = f.handle.GetAttributeBool(shared.AttributeIDConnectionMtls)
	)

	return map[string]any{
		"attributes": map[string]any{
			"request": map[string]any{
				"http": map[string]any{
					"method":   method,
					"path":     path,
					"headers":  headerMap,
					"host":     host,
					"scheme":   scheme,
					"protocol": protocol,
				},
			},
			"source": map[string]any{
				"address": sourceAddr,
				"certificate": map[string]any{
					"uri_san":       uriSanPeer,
					"dns_san":       dnsSanPeer,
					"subject":       subjectPeer,
					"sha256_digest": sha256Digest,
				},
			},
			"destination": map[string]any{
				"address": destAddr,
			},
			"connection": map[string]any{
				// TODO(nacx): Add mTLS boolean when supported by the ABI.
				// "mtls":        mtls,
				"tls_version": tlsVersion,
			},
		},
		"parsed_path":  parsedPath,
		"parsed_query": parsedQuery,
	}
}

// parsePath splits the path into segments and parses query parameters into a map.
func parsePath(fullPath string) ([]string, map[string][]string) {
	pathPart := fullPath
	queryPart := ""
	if before, after, ok := strings.Cut(fullPath, "?"); ok {
		pathPart = before
		queryPart = after
	}

	// Split path into segments, trimming leading slash.
	segments := strings.Split(strings.TrimPrefix(pathPart, "/"), "/")

	// Parse query parameters using net/url for proper decoding.
	queryMap := make(map[string][]string)
	if queryPart != "" {
		parsed, err := url.ParseQuery(queryPart)
		if err == nil {
			maps.Copy(queryMap, parsed)
		}
	}

	return segments, queryMap
}

// interpretResult processes the OPA evaluation result and extracts the allowed boolean and optional response details.
func interpretResult(rs rego.ResultSet) (bool, policyResponse) {
	resp := policyResponse{}

	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return false, resp
	}

	result := rs[0].Bindings["result"]

	switch v := result.(type) {
	case bool:
		return v, resp

	case map[string]any:
		allowed, _ := v["allowed"].(bool)

		if httpStatus, ok := v["http_status"].(json.Number); ok {
			if n, err := httpStatus.Int64(); err == nil {
				resp.httpStatus = int(n)
			}
		} else if httpStatus, ok := v["http_status"].(float64); ok {
			resp.httpStatus = int(httpStatus)
		}

		if body, ok := v["body"].(string); ok {
			resp.body = body
		}

		if headers, ok := v["headers"].(map[string]any); ok {
			resp.headers = make(map[string]string)
			for k, val := range headers {
				if s, ok := val.(string); ok {
					resp.headers[k] = s
				}
			}
		}

		return allowed, resp

	default:
		return false, resp
	}
}

// opaHttpFilterFactory creates filter instances per-request.
type opaHttpFilterFactory struct { //nolint:revive
	shared.EmptyHttpFilterFactory
	config *opaParsedConfig
}

func (o *opaHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &opaHttpFilter{handle: handle, config: o.config}
}

// OPAHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type OPAHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON configuration and creates a factory for the HTTP filter.
func (o *OPAHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "opa: empty config")
		return nil, fmt.Errorf("empty config")
	}

	cfg := opaConfig{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "opa: failed to parse config: %s", err.Error())
		return nil, err
	}

	if len(cfg.PolicyFiles) == 0 && len(cfg.InlinePolicies) == 0 {
		handle.Log(shared.LogLevelError, "opa: either policy_files or inline_policies must be provided")
		return nil, fmt.Errorf("either policy_files or inline_policies must be provided")
	}

	modules := make(map[string]string, len(cfg.PolicyFiles)+len(cfg.InlinePolicies))

	// Load policies from files.
	for _, path := range cfg.PolicyFiles {
		handle.Log(shared.LogLevelDebug, "opa: loading policy file %s", path)
		policyBytes, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			handle.Log(shared.LogLevelError, "opa: failed to read policy file %s: %s", path, err.Error())
			return nil, fmt.Errorf("failed to read policy file %s: %w", path, err)
		}
		modules[path] = string(policyBytes)
	}

	// Add inline policies.
	for i, p := range cfg.InlinePolicies {
		handle.Log(shared.LogLevelDebug, "opa: adding inline policy #%d", i+1)
		modules[fmt.Sprintf("inline_policy_%d.rego", i+1)] = p
	}

	handle.Log(shared.LogLevelDebug, "opa: loaded %d policies (decision_path=%s, dry_run=%v, fail_open=%v)",
		len(modules), cfg.DecisionPath, cfg.DryRun, cfg.FailOpen)

	if cfg.DecisionPath == "" {
		cfg.DecisionPath = defaultDecisionPath
	}
	query := "result = data." + cfg.DecisionPath
	handle.Log(shared.LogLevelDebug, "opa: compiling query: %s", query)

	opts := []func(*rego.Rego){rego.Query(query)}
	for name, module := range modules {
		opts = append(opts, rego.Module(name, module))
	}
	r := rego.New(opts...)

	pq, err := r.PrepareForEval(context.Background())
	if err != nil {
		handle.Log(shared.LogLevelError, "opa: failed to compile policy: %s", err.Error())
		return nil, fmt.Errorf("failed to compile policy: %w", err)
	}

	handle.Log(shared.LogLevelDebug, "opa: policy compiled successfully")

	var metrics opaMetrics
	metricID, metricStatus := handle.DefineCounter("opa_requests_total", "decision")
	if metricStatus == shared.MetricsSuccess {
		metrics.requestsTotal = metricID
		metrics.enabled = true
	}

	parsed := &opaParsedConfig{
		opaConfig:     cfg,
		preparedQuery: pq,
		metrics:       metrics,
	}

	return &opaHttpFilterFactory{config: parsed}, nil
}

// ExtensionName is the name of the extension that will be used in the
// `run` command to refer to this plugin.
const ExtensionName = "opa"

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &OPAHttpFilterConfigFactory{},
	}
}
