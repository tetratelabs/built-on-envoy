// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package cedar implements a Cedar authorization HTTP filter plugin.
package cedar

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	cedarlib "github.com/cedar-policy/cedar-go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// cedarConfig represents the JSON configuration for this filter.
type cedarConfig struct {
	// PolicyFile is the path to the .cedar policy file.
	PolicyFile string `json:"policy_file"`
	// EntitiesFile is the optional path to a JSON entities file.
	EntitiesFile string `json:"entities_file"`
	// PrincipalType is the Cedar entity type for the principal (e.g., "User").
	PrincipalType string `json:"principal_type"`
	// PrincipalIDHeader is the request header whose value becomes the principal's ID.
	PrincipalIDHeader string `json:"principal_id_header"`
	// ActionType is the entity type for actions (default: "Action").
	ActionType string `json:"action_type"`
	// ResourceType is the entity type for resources (default: "Resource").
	ResourceType string `json:"resource_type"`
	// DenyStatus is the HTTP status code to return on deny (default: 403).
	DenyStatus int `json:"deny_status"`
	// DenyBody is the response body to return on deny (default: "Forbidden").
	DenyBody string `json:"deny_body"`
	// DenyHeaders are additional headers to include in the deny response.
	DenyHeaders map[string]string `json:"deny_headers"`
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

// cedarMetrics holds the metric IDs for the Cedar filter.
type cedarMetrics struct {
	requestsTotal shared.MetricID
	enabled       bool
}

// IncRequestsTotal increments the requests counter with the given decision tag value.
func (m cedarMetrics) IncRequestsTotal(handle shared.HttpFilterHandle, decision string) {
	if m.enabled {
		handle.IncrementCounterValue(m.requestsTotal, 1, decision)
	}
}

// cedarParsedConfig holds the parsed configuration and compiled Cedar policy set.
type cedarParsedConfig struct {
	cedarConfig
	policySet *cedarlib.PolicySet
	entities  cedarlib.EntityMap
	metrics   cedarMetrics
}

// cedarHttpFilter is the per-request HTTP filter instance.
type cedarHttpFilter struct { //nolint:revive
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *cedarParsedConfig
}

func (c *cedarHttpFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	req, err := c.buildRequest(headers)
	if err != nil {
		if c.config.FailOpen {
			c.handle.Log(shared.LogLevelWarn, "cedar: request building error (fail_open enabled): %s", err.Error())
			c.config.metrics.IncRequestsTotal(c.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		c.handle.Log(shared.LogLevelWarn, "cedar: request building error: %s", err.Error())
		c.handle.SendLocalResponse(403, nil, []byte("Forbidden"), "cedar_denied")
		c.config.metrics.IncRequestsTotal(c.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	c.handle.Log(shared.LogLevelDebug, "cedar: evaluating policy for %s %s",
		headers.GetOne(":method"), headers.GetOne(":path"))

	decision, diagnostic := cedarlib.Authorize(c.config.policySet, c.config.entities, req)

	c.handle.Log(shared.LogLevelDebug, "cedar: decision=%s diagnostic=%+v", decision, diagnostic)

	if len(diagnostic.Errors) > 0 {
		if c.config.FailOpen {
			c.handle.Log(shared.LogLevelError, "cedar: policy evaluation errors (fail_open enabled): %v", diagnostic.Errors)
			c.config.metrics.IncRequestsTotal(c.handle, decisionFailOpen)
			return shared.HeadersStatusContinue
		}
		c.handle.Log(shared.LogLevelError, "cedar: policy evaluation errors: %v", diagnostic.Errors)
		c.handle.SendLocalResponse(500, nil, []byte("Internal Server Error"), "cedar_eval_error")
		c.config.metrics.IncRequestsTotal(c.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	allowed := decision == cedarlib.Allow

	if !allowed && c.config.DryRun {
		c.handle.Log(shared.LogLevelInfo, "cedar: dry-run decision: allowed=%v", allowed)
		c.config.metrics.IncRequestsTotal(c.handle, decisionDryAllow)
		allowed = true
	}

	if !allowed {
		status := c.config.DenyStatus
		if status == 0 {
			status = 403
		}
		body := c.config.DenyBody
		if body == "" {
			body = "Forbidden"
		}
		var responseHeaders [][2]string
		for k, v := range c.config.DenyHeaders {
			responseHeaders = append(responseHeaders, [2]string{k, v})
		}
		c.handle.Log(shared.LogLevelDebug, "cedar: denying request with status %d", status)
		c.handle.SendLocalResponse(
			uint32(status), //nolint:gosec
			responseHeaders,
			[]byte(body),
			"cedar_denied",
		)
		c.config.metrics.IncRequestsTotal(c.handle, decisionDenied)
		return shared.HeadersStatusStop
	}

	if !c.config.DryRun {
		c.config.metrics.IncRequestsTotal(c.handle, decisionAllowed)
	}

	return shared.HeadersStatusContinue
}

// buildRequest constructs the Cedar authorization request from HTTP headers and attributes.
func (c *cedarHttpFilter) buildRequest(headers shared.HeaderMap) (cedarlib.Request, error) {
	principalID := ""
	if c.config.PrincipalIDHeader != "" {
		principalID = headers.GetOne(c.config.PrincipalIDHeader)
	}
	if principalID == "" {
		return cedarlib.Request{}, fmt.Errorf("principal header %q is empty or missing", c.config.PrincipalIDHeader)
	}

	principal := cedarlib.NewEntityUID(
		cedarlib.EntityType(c.config.PrincipalType),
		cedarlib.String(principalID),
	)

	method := headers.GetOne(":method")
	actionType := cmp.Or(c.config.ActionType, "Action")
	action := cedarlib.NewEntityUID(
		cedarlib.EntityType(actionType),
		cedarlib.String(method),
	)

	fullPath := headers.GetOne(":path")
	resourcePath := fullPath
	if before, _, ok := strings.Cut(fullPath, "?"); ok {
		resourcePath = before
	}
	resourceType := cmp.Or(c.config.ResourceType, "Resource")
	resource := cedarlib.NewEntityUID(
		cedarlib.EntityType(resourceType),
		cedarlib.String(resourcePath),
	)

	context := c.buildContext(headers)

	return cedarlib.Request{
		Principal: principal,
		Action:    action,
		Resource:  resource,
		Context:   context,
	}, nil
}

// buildContext constructs the Cedar context record from request headers and Envoy attributes.
func (c *cedarHttpFilter) buildContext(headers shared.HeaderMap) cedarlib.Record {
	var (
		method = headers.GetOne(":method")
		path   = headers.GetOne(":path")
		host   = headers.GetOne(":authority")
		scheme = cmp.Or(headers.GetOne(":scheme"), "http")
	)
	parsedPath, parsedQuery := parsePath(path)
	protocol, _ := c.handle.GetAttributeString(shared.AttributeIDRequestProtocol)
	protocol = cmp.Or(protocol, "HTTP/1.1")

	// Build headers record excluding pseudo-headers.
	headerMap := cedarlib.RecordMap{}
	for _, h := range headers.GetAll() {
		key := h[0]
		val := h[1]
		if !strings.HasPrefix(key, ":") {
			headerMap[cedarlib.String(key)] = cedarlib.String(val)
		}
	}

	var (
		sourceAddr, _ = c.handle.GetAttributeString(shared.AttributeIDSourceAddress)
		destAddr, _   = c.handle.GetAttributeString(shared.AttributeIDDestinationAddress)
		// Extract connection/TLS attributes for mTLS-aware policies.
		uriSanPeer, _   = c.handle.GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate)
		dnsSanPeer, _   = c.handle.GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate)
		subjectPeer, _  = c.handle.GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate)
		tlsVersion, _   = c.handle.GetAttributeString(shared.AttributeIDConnectionTlsVersion)
		sha256Digest, _ = c.handle.GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest)
	)

	// Build parsed_path as a Cedar Set of Strings.
	pathValues := make([]cedarlib.Value, len(parsedPath))
	for i, seg := range parsedPath {
		pathValues[i] = cedarlib.String(seg)
	}

	// Build parsed_query as a Cedar Record of Sets.
	queryRecord := cedarlib.RecordMap{}
	for k, vals := range parsedQuery {
		qv := make([]cedarlib.Value, len(vals))
		for i, v := range vals {
			qv[i] = cedarlib.String(v)
		}
		queryRecord[cedarlib.String(k)] = cedarlib.NewSet(qv...)
	}

	return cedarlib.NewRecord(cedarlib.RecordMap{
		"request": cedarlib.NewRecord(cedarlib.RecordMap{
			"method":   cedarlib.String(method),
			"path":     cedarlib.String(path),
			"host":     cedarlib.String(host),
			"scheme":   cedarlib.String(scheme),
			"protocol": cedarlib.String(protocol),
			"headers":  cedarlib.NewRecord(headerMap),
		}),
		"source": cedarlib.NewRecord(cedarlib.RecordMap{
			"address": cedarlib.String(sourceAddr),
			"certificate": cedarlib.NewRecord(cedarlib.RecordMap{
				"uri_san":       cedarlib.String(uriSanPeer),
				"dns_san":       cedarlib.String(dnsSanPeer),
				"subject":       cedarlib.String(subjectPeer),
				"sha256_digest": cedarlib.String(sha256Digest),
			}),
		}),
		"destination": cedarlib.NewRecord(cedarlib.RecordMap{
			"address": cedarlib.String(destAddr),
		}),
		"connection": cedarlib.NewRecord(cedarlib.RecordMap{
			"tls_version": cedarlib.String(tlsVersion),
		}),
		"parsed_path":  cedarlib.NewSet(pathValues...),
		"parsed_query": cedarlib.NewRecord(queryRecord),
	})
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
			for k, v := range parsed {
				queryMap[k] = v
			}
		}
	}

	return segments, queryMap
}

// cedarHttpFilterFactory creates filter instances per-request.
type cedarHttpFilterFactory struct { //nolint:revive
	shared.EmptyHttpFilterFactory
	config *cedarParsedConfig
}

func (c *cedarHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &cedarHttpFilter{handle: handle, config: c.config}
}

// CedarHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type CedarHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON configuration and creates a factory for the HTTP filter.
func (c *CedarHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "cedar: empty config")
		return nil, fmt.Errorf("empty config")
	}

	cfg := cedarConfig{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "cedar: failed to parse config: %s", err.Error())
		return nil, err
	}

	if cfg.PolicyFile == "" {
		handle.Log(shared.LogLevelError, "cedar: policy_file is required")
		return nil, fmt.Errorf("policy_file is required")
	}

	if cfg.PrincipalType == "" {
		handle.Log(shared.LogLevelError, "cedar: principal_type is required")
		return nil, fmt.Errorf("principal_type is required")
	}

	if cfg.PrincipalIDHeader == "" {
		handle.Log(shared.LogLevelError, "cedar: principal_id_header is required")
		return nil, fmt.Errorf("principal_id_header is required")
	}

	handle.Log(shared.LogLevelDebug, "cedar: loading policy from %s (principal_type=%s, principal_id_header=%s, dry_run=%v, fail_open=%v)",
		cfg.PolicyFile, cfg.PrincipalType, cfg.PrincipalIDHeader, cfg.DryRun, cfg.FailOpen)

	policyBytes, err := os.ReadFile(cfg.PolicyFile)
	if err != nil {
		handle.Log(shared.LogLevelError, "cedar: failed to read policy file: %s", err.Error())
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}

	policySet, err := cedarlib.NewPolicySetFromBytes("policy.cedar", policyBytes)
	if err != nil {
		handle.Log(shared.LogLevelError, "cedar: failed to parse policy: %s", err.Error())
		return nil, fmt.Errorf("failed to parse policy: %w", err)
	}

	handle.Log(shared.LogLevelDebug, "cedar: policy parsed successfully")

	// Load entities if provided.
	var entities cedarlib.EntityMap
	if cfg.EntitiesFile != "" {
		entitiesBytes, err := os.ReadFile(cfg.EntitiesFile)
		if err != nil {
			handle.Log(shared.LogLevelError, "cedar: failed to read entities file: %s", err.Error())
			return nil, fmt.Errorf("failed to read entities file: %w", err)
		}
		if err := json.Unmarshal(entitiesBytes, &entities); err != nil {
			handle.Log(shared.LogLevelError, "cedar: failed to parse entities: %s", err.Error())
			return nil, fmt.Errorf("failed to parse entities: %w", err)
		}
		handle.Log(shared.LogLevelDebug, "cedar: entities loaded successfully")
	}

	var metrics cedarMetrics
	metricID, metricStatus := handle.DefineCounter("cedar_requests_total", "decision")
	if metricStatus == shared.MetricsSuccess {
		metrics.requestsTotal = metricID
		metrics.enabled = true
	}

	parsed := &cedarParsedConfig{
		cedarConfig: cfg,
		policySet:   policySet,
		entities:    entities,
		metrics:     metrics,
	}

	return &cedarHttpFilterFactory{config: parsed}, nil
}

// ExtensionName is the name of the extension that will be used in the
// `run` command to refer to this plugin.
const ExtensionName = "cedar-auth"

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &CedarHttpFilterConfigFactory{},
	}
}
