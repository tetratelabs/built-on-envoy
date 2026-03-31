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
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"
	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// defaultDecisionPath is the default OPA rule path to query if not specified in config.
const defaultDecisionPath = "envoy.authz.allow"

// opaConfig represents the JSON configuration for this filter.
type opaConfig struct {
	// Policies contains the OPA policies to load, which can be specified as either inline strings or file paths.
	Policies []pkg.DataSource `json:"policies"`
	// DecisionPath is the OPA rule path to query (default: "envoy.authz.allow").
	DecisionPath string `json:"decision_path"`
	// FailOpen allows requests if there is an error evaluating the policy.
	// If false, errors will result in a 500 response.
	FailOpen bool `json:"fail_open"`
	// DryRun when true logs the decision but always allows the request.
	DryRun bool `json:"dry_run"`
	// WithBody when true buffers the request body and includes it as parsed JSON in the OPA
	// input document under the "body" key. Only JSON bodies are supported; non-JSON bodies
	// result in "body" being absent from the input. When false (default), the policy is
	// evaluated on request headers only.
	WithBody bool `json:"with_body"`
	// MetadataNamespaces is an optional list of dynamic metadata namespaces to include in the OPA
	// input document under the "dynamic_metadata" key.
	MetadataNamespaces []string `json:"metadata_namespaces"`
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
	// requestProcessed is set to true once the policy has been evaluated to prevent re-evaluation.
	requestProcessed bool
}

// policyResponse holds optional structured response details from the policy.
type policyResponse struct {
	httpStatus int
	headers    map[string]string
	body       string
}

func (o *opaHttpFilter) OnRequestHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if o.config.WithBody && !endOfStream {
		// Wait for the request body before evaluating the policy.
		return shared.HeadersStatusStop
	}
	if !o.evaluateAndDecide(headers, nil) {
		return shared.HeadersStatusStop
	}
	return shared.HeadersStatusContinue
}

// OnRequestBody buffers the request body and evaluates the OPA policy once the full body is received.
// This is only invoked when with_body is true in the configuration.
func (o *opaHttpFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if o.requestProcessed {
		return shared.BodyStatusContinue
	}

	if !endOfStream {
		// Keep buffering until the full body is received.
		return shared.BodyStatusStopAndBuffer
	}

	if !o.evaluateBodyPolicy() {
		return shared.BodyStatusStopNoBuffer
	}
	return shared.BodyStatusContinue
}

// OnRequestTrailers is called when request trailers are received, signaling the end of the request body.
// This is only invoked when with_body is true in the configuration.
func (o *opaHttpFilter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if o.requestProcessed {
		return shared.TrailersStatusContinue
	}

	if !o.evaluateBodyPolicy() {
		return shared.TrailersStatusStop
	}
	return shared.TrailersStatusContinue
}

// evaluateAndDecide evaluates the OPA policy with the given headers and optional parsed body,
// enforces the decision, and returns true if the request is allowed.
func (o *opaHttpFilter) evaluateAndDecide(headers shared.HeaderMap, parsedBody any) bool {
	o.requestProcessed = true

	input := o.buildInput(headers, parsedBody)
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
			return true
		}
		o.handle.Log(shared.LogLevelError, "opa: policy evaluation error: %s", err.Error())
		o.handle.SendLocalResponse(500, nil, []byte("Internal Server Error"), "opa_eval_error")
		o.config.metrics.IncRequestsTotal(o.handle, decisionDenied)
		return false
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
		o.config.metrics.IncRequestsTotal(o.handle, decisionDenied)
		o.handle.SendLocalResponse(
			uint32(status), //nolint:gosec
			responseHeaders,
			[]byte(body),
			"opa_denied",
		)
		return false
	}

	// If allowed and policy returned headers, add them to the request.
	for k, v := range resp.headers {
		o.handle.Log(shared.LogLevelDebug, "opa: adding header %s=%s", k, v)
		o.handle.RequestHeaders().Set(k, v)
	}

	if !o.config.DryRun {
		o.config.metrics.IncRequestsTotal(o.handle, decisionAllowed)
	}

	return true
}

// evaluateBodyPolicy reads the buffered request body, parses it as JSON if the content-type is
// application/json, and evaluates the OPA policy.
func (o *opaHttpFilter) evaluateBodyPolicy() bool {
	headers := o.handle.RequestHeaders()

	var parsedBody any
	contentType := headers.GetOne("content-type").ToUnsafeString()
	// We may could support other content types in the future if needed, but for now we only parse
	// JSON bodies since that's the most common and avoids the complexity of handling arbitrary
	// body formats.
	if strings.Contains(contentType, "application/json") {
		bodyBytes := utility.ReadWholeRequestBody(o.handle)
		if len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
				o.handle.Log(shared.LogLevelDebug, "opa: failed to parse JSON body: %s", err.Error())
			}
		}
	}

	return o.evaluateAndDecide(headers, parsedBody)
}

// buildInput constructs the input document for OPA evaluation based on request headers, attributes,
// and an optional pre-parsed JSON body.
func (o *opaHttpFilter) buildInput(headers shared.HeaderMap, parsedBody any) map[string]any {
	var (
		method = headers.GetOne(":method").ToUnsafeString()
		path   = headers.GetOne(":path").ToUnsafeString()
		host   = headers.GetOne(":authority").ToUnsafeString()
		scheme = cmp.Or(headers.GetOne(":scheme").ToUnsafeString(), "http")
	)

	parsedPath, parsedQuery := parsePath(path)
	protocolAttr, _ := o.handle.GetAttributeString(shared.AttributeIDRequestProtocol)
	protocol := cmp.Or(protocolAttr.ToUnsafeString(), "HTTP/1.1")

	// Build headers map excluding pseudo-headers.
	headerMap := make(map[string]string)
	for _, h := range headers.GetAll() {
		key := h[0].ToUnsafeString()
		val := h[1].ToUnsafeString()
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
		mtls, _         = o.handle.GetAttributeBool(shared.AttributeIDConnectionMtls)
	)

	result := map[string]any{
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
				"address": sourceAddr.ToUnsafeString(),
				"certificate": map[string]any{
					"uri_san":       uriSanPeer.ToUnsafeString(),
					"dns_san":       dnsSanPeer.ToUnsafeString(),
					"subject":       subjectPeer.ToUnsafeString(),
					"sha256_digest": sha256Digest.ToUnsafeString(),
				},
			},
			"destination": map[string]any{
				"address": destAddr.ToUnsafeString(),
			},
			"connection": map[string]any{
				"mtls":        mtls,
				"tls_version": tlsVersion.ToUnsafeString(),
			},
		},
		"dynamic_metadata": o.dynamicMetadataMap(),
		"parsed_path":      parsedPath,
		"parsed_query":     parsedQuery,
	}
	if parsedBody != nil {
		result["body"] = parsedBody
	}
	return result
}

// dynamicMetadataMap extracts dynamic metadata from the filter handle and returns it as a
// nested map keyed by namespace and then by key.
func (o *opaHttpFilter) dynamicMetadataMap() map[string]any {
	dm := make(map[string]any)
	for _, ns := range o.config.MetadataNamespaces {
		nsMap := make(map[string]any)
		keys := o.handle.GetMetadataKeys(shared.MetadataSourceTypeDynamic, ns)
		for _, key := range keys {
			keyStr := key.ToUnsafeString()
			if value, ok := o.handle.GetMetadataString(shared.MetadataSourceTypeDynamic, ns, keyStr); ok {
				nsMap[keyStr] = value.ToUnsafeString()
			} else if numValue, ok := o.handle.GetMetadataNumber(shared.MetadataSourceTypeDynamic, ns, keyStr); ok {
				nsMap[keyStr] = numValue
			}
		}
		if len(nsMap) > 0 {
			dm[ns] = nsMap
		}
	}
	return dm
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

	if len(cfg.Policies) == 0 {
		handle.Log(shared.LogLevelError, "opa: no policies provided in config")
		return nil, fmt.Errorf("no policies provided in config")
	}

	modules := make(map[string]string, len(cfg.Policies))

	for i, p := range cfg.Policies {
		content, err := p.Content()
		if err != nil {
			handle.Log(shared.LogLevelError, "opa: failed to load policy #%d: %s", i+1, err.Error())
			return nil, fmt.Errorf("failed to load policy #%d: %w", i+1, err)
		}
		moduleName := p.File
		if moduleName == "" {
			moduleName = fmt.Sprintf("inline_policy_%d.rego", i+1)
		}
		handle.Log(shared.LogLevelDebug, "opa: loaded policy #%d (source=%s)", i+1, moduleName)
		modules[moduleName] = string(content)
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
