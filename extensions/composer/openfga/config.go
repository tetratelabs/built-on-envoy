// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package openfga implements an OpenFGA authorization HTTP filter plugin.
package openfga

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const defaultTimeoutMs uint64 = 5000

// openfgaConfig holds the JSON configuration for this filter.
type openfgaConfig struct {
	Cluster              string                 `json:"cluster"`
	OpenFGAHost          string                 `json:"openfga_host"`
	StoreID              string                 `json:"store_id"`
	AuthorizationModelID string                 `json:"authorization_model_id"`
	User                 valueSource            `json:"user"`
	Relation             valueSource            `json:"relation"`
	Object               valueSource            `json:"object"`
	Rules                []checkRule            `json:"rules"`
	FailOpen             bool                   `json:"fail_open"`
	DryRun               bool                   `json:"dry_run"`
	TimeoutMs            uint64                 `json:"timeout_ms"`
	DenyStatus           int                    `json:"deny_status"`
	DenyBody             string                 `json:"deny_body"`
	Consistency          string                 `json:"consistency,omitempty"`
	Metadata             *pkg.MetadataKey       `json:"metadata,omitempty"`
	CalloutHeaders       map[string]string      `json:"callout_headers,omitempty"`
	ContextualTuples     []contextualTupleCfg   `json:"contextual_tuples,omitempty"`
	Context              map[string]valueSource `json:"context,omitempty"`
}

// valueSource defines how to extract a value from the request.
// Exactly one of Value, Header, PathSegment, or QueryParam must be set.
// Prefix is optional and prepended to the result.
type valueSource struct {
	Value       string `json:"value"`
	Header      string `json:"header"`
	PathSegment *int   `json:"path_segment,omitempty"` // 0-indexed; negative values count from end (-1 = last)
	QueryParam  string `json:"query_param,omitempty"`
	Prefix      string `json:"prefix"`
	resolved    string // set at config time when Value is static; used by resolve to avoid per-request work
}

func (v *valueSource) isEmpty() bool {
	return v.Value == "" && v.Header == "" && v.PathSegment == nil && v.QueryParam == ""
}

// precompute sets resolved when Value is static, so resolve can return it without per-request work.
func (v *valueSource) precompute() {
	if v.Value != "" {
		v.resolved = v.Prefix + v.Value
	}
}

// resolve extracts the value from the given request headers using the configured source.
func (v *valueSource) resolve(headers shared.HeaderMap) string {
	if v.resolved != "" {
		return v.resolved
	}
	var raw string
	switch {
	case v.Value != "":
		raw = v.Value
	case v.Header != "":
		raw = headers.GetOne(v.Header).ToUnsafeString()
	case v.PathSegment != nil:
		raw = extractPathSegment(headers.GetOne(":path").ToUnsafeString(), *v.PathSegment)
	case v.QueryParam != "":
		raw = extractQueryParam(headers.GetOne(":path").ToUnsafeString(), v.QueryParam)
	}
	if raw == "" {
		return ""
	}
	return v.Prefix + raw
}

// contextualTupleCfg defines how to extract a contextual tuple from the request.
type contextualTupleCfg struct {
	User     valueSource `json:"user"`
	Relation valueSource `json:"relation"`
	Object   valueSource `json:"object"`
}

// parsedContextualTuple is a validated, ready-to-use contextual tuple source.
type parsedContextualTuple struct {
	user     valueSource
	relation valueSource
	object   valueSource
}

// resolvedTuple is a fully resolved tuple with concrete values.
type resolvedTuple struct {
	User     string
	Relation string
	Object   string
}

// ruleMatch defines conditions that must all be true for a rule to apply.
// "*" means the header must be present with any non-empty value; any other
// string requires an exact match.
type ruleMatch struct {
	Headers map[string]string `json:"headers"`
}

func (m *ruleMatch) matches(headers shared.HeaderMap) bool {
	for name, want := range m.Headers {
		got := headers.GetOne(name).ToUnsafeString()
		if got == "" {
			return false
		}
		if want != "*" && got != want {
			return false
		}
	}
	return true
}

// checkRule defines how to build the OpenFGA Check tuple for a matched request.
type checkRule struct {
	Match    *ruleMatch   `json:"match,omitempty"`
	User     *valueSource `json:"user,omitempty"`
	Relation valueSource  `json:"relation"`
	Object   valueSource  `json:"object"`
}

// parsedRule is a validated, ready-to-use rule with defaults merged in.
type parsedRule struct {
	match    *ruleMatch
	user     valueSource
	relation valueSource
	object   valueSource
}

// parsedConfig holds the validated configuration and precomputed callout headers.
type parsedConfig struct {
	cluster              string
	storeID              string
	authorizationModelID string
	failOpen             bool
	dryRun               bool
	timeoutMs            uint64
	deny                 pkg.LocalResponse
	denyHeaders          [][2]string
	denyBodyBytes        []byte
	checkPath            string
	calloutHeaders       [][2]string
	rules                []parsedRule
	metadata             *pkg.MetadataKey
	consistency          string // OpenFGA Check consistency; empty means omit from JSON body
	contextualTuples     []parsedContextualTuple
	context              map[string]valueSource // key = context field name
}

func parseConfig(data []byte) (*parsedConfig, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("openfga: configuration is required")
	}

	cfg := &openfgaConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("openfga: failed to parse config: %w", err)
	}

	var errs []error
	if cfg.Cluster == "" {
		errs = append(errs, fmt.Errorf("missing required field: cluster"))
	}
	if cfg.OpenFGAHost == "" {
		errs = append(errs, fmt.Errorf("missing required field: openfga_host"))
	}
	if cfg.StoreID == "" {
		errs = append(errs, fmt.Errorf("missing required field: store_id"))
	}
	if err := validateConsistency(cfg.Consistency); err != nil {
		errs = append(errs, err)
	}

	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = defaultTimeoutMs
	}

	denyStatus := cfg.DenyStatus
	if denyStatus == 0 {
		denyStatus = 403
	}
	denyStatusValid := true
	if denyStatus < 100 || denyStatus > 599 {
		errs = append(errs, fmt.Errorf("openfga: deny_status must be between 100 and 599, got %d", denyStatus))
		denyStatusValid = false
	}
	denyBody := cfg.DenyBody
	if denyBody == "" {
		denyBody = "Forbidden"
	}
	var deny pkg.LocalResponse
	var denyHeaders [][2]string
	if denyStatusValid {
		deny = pkg.LocalResponse{Status: denyStatus, Body: denyBody}
		if err := deny.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("openfga: %w", err))
		} else {
			denyHeaders = buildDenyHeaders(deny)
		}
	}

	if cfg.Metadata != nil {
		if err := cfg.Metadata.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("openfga: invalid metadata config: %w", err))
		}
	}

	rules, rulesErr := buildRules(cfg)
	if rulesErr != nil {
		errs = append(errs, rulesErr)
	}

	ctxTuples, ctxTuplesErr := buildContextualTuples(cfg.ContextualTuples)
	if ctxTuplesErr != nil {
		errs = append(errs, ctxTuplesErr)
	}

	ctxMap, ctxMapErr := buildContext(cfg.Context)
	if ctxMapErr != nil {
		errs = append(errs, ctxMapErr)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}

	checkPath := "/stores/" + cfg.StoreID + "/check"

	calloutHeaders := [][2]string{
		{":method", "POST"},
		{":path", checkPath},
		{"host", cfg.OpenFGAHost},
		{"content-type", "application/json"},
	}
	for k, v := range cfg.CalloutHeaders {
		calloutHeaders = append(calloutHeaders, [2]string{k, v})
	}

	return &parsedConfig{
		cluster:              cfg.Cluster,
		storeID:              cfg.StoreID,
		authorizationModelID: cfg.AuthorizationModelID,
		failOpen:             cfg.FailOpen,
		dryRun:               cfg.DryRun,
		timeoutMs:            cfg.TimeoutMs,
		deny:                 deny,
		denyHeaders:          denyHeaders,
		denyBodyBytes:        []byte(denyBody),
		checkPath:            checkPath,
		metadata:             cfg.Metadata,
		consistency:          cfg.Consistency,
		calloutHeaders:       calloutHeaders,
		rules:                rules,
		contextualTuples:     ctxTuples,
		context:              ctxMap,
	}, nil
}

// buildRules normalizes the config into a list of parsedRules.
// When cfg.Rules is empty, the top-level User/Relation/Object are used as a
// single catch-all rule (backward-compatible legacy mode).
func buildRules(cfg *openfgaConfig) ([]parsedRule, error) {
	if len(cfg.Rules) == 0 {
		return buildLegacyRule(cfg)
	}

	var errs []error
	if cfg.User.isEmpty() {
		errs = append(errs, fmt.Errorf("top-level user is required when using rules (serves as default)"))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}

	rules := make([]parsedRule, 0, len(cfg.Rules))
	catchAllSeen := false
	for i := range cfg.Rules {
		cr := &cfg.Rules[i]
		if catchAllSeen {
			return nil, fmt.Errorf("openfga: rule[%d]: catch-all rule (no match) must be last", i)
		}
		if cr.Match == nil || len(cr.Match.Headers) == 0 {
			catchAllSeen = true
		}

		user := cfg.User
		if cr.User != nil {
			user = *cr.User
		}

		var ruleErrs []error
		if err := validateValueSource(fmt.Sprintf("rule[%d].user", i), &user); err != nil {
			ruleErrs = append(ruleErrs, err)
		}
		if err := validateValueSource(fmt.Sprintf("rule[%d].relation", i), &cr.Relation); err != nil {
			ruleErrs = append(ruleErrs, err)
		}
		if err := validateValueSource(fmt.Sprintf("rule[%d].object", i), &cr.Object); err != nil {
			ruleErrs = append(ruleErrs, err)
		}
		if len(ruleErrs) > 0 {
			errs = append(errs, ruleErrs...)
			continue
		}

		var match *ruleMatch
		if cr.Match != nil && len(cr.Match.Headers) > 0 {
			match = cr.Match
		}

		user.precompute()
		cr.Relation.precompute()
		cr.Object.precompute()
		rules = append(rules, parsedRule{
			match:    match,
			user:     user,
			relation: cr.Relation,
			object:   cr.Object,
		})
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}
	return rules, nil
}

// warnStoreIDFormat returns a warning message if the store ID does not look like a ULID.
func warnStoreIDFormat(storeID string) string {
	if len(storeID) != 26 {
		return fmt.Sprintf("openfga: store_id %q does not look like a ULID (expected 26 characters, got %d); verify it is correct", storeID, len(storeID))
	}
	return ""
}

// validateConsistency ensures cfg matches OpenFGA ConsistencyPreference JSON values.
func validateConsistency(s string) error {
	if s == "" {
		return nil
	}
	switch s {
	case "UNSPECIFIED", "MINIMIZE_LATENCY", "HIGHER_CONSISTENCY":
		return nil
	default:
		return fmt.Errorf("openfga: invalid consistency %q, must be UNSPECIFIED, MINIMIZE_LATENCY, or HIGHER_CONSISTENCY", s)
	}
}

// buildLegacyRule creates a single catch-all rule from the top-level fields.
func buildLegacyRule(cfg *openfgaConfig) ([]parsedRule, error) {
	var errs []error
	if err := validateValueSource("user", &cfg.User); err != nil {
		errs = append(errs, err)
	}
	if err := validateValueSource("relation", &cfg.Relation); err != nil {
		errs = append(errs, err)
	}
	if err := validateValueSource("object", &cfg.Object); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}
	cfg.User.precompute()
	cfg.Relation.precompute()
	cfg.Object.precompute()
	return []parsedRule{{
		user:     cfg.User,
		relation: cfg.Relation,
		object:   cfg.Object,
	}}, nil
}

func validateValueSource(name string, vs *valueSource) error {
	set := 0
	if vs.Value != "" {
		set++
	}
	if vs.Header != "" {
		set++
	}
	if vs.PathSegment != nil {
		set++
	}
	if vs.QueryParam != "" {
		set++
	}
	if set == 0 {
		return fmt.Errorf("%s: one of 'value', 'header', 'path_segment', or 'query_param' must be set", name)
	}
	if set > 1 {
		return fmt.Errorf("%s: only one of 'value', 'header', 'path_segment', or 'query_param' may be set", name)
	}
	return nil
}

// buildDenyHeaders precomputes the [][2]string headers for deny responses.
func buildDenyHeaders(deny pkg.LocalResponse) [][2]string {
	headers := make([][2]string, 0, len(deny.Headers)+1)
	hasContentType := false
	for k, v := range deny.Headers {
		if k == "content-type" {
			hasContentType = true
		}
		headers = append(headers, [2]string{k, v})
	}
	if !hasContentType {
		headers = append([][2]string{{"content-type", "text/plain"}}, headers...)
	}
	return headers
}

// buildContextualTuples validates and prepares contextual tuple sources.
func buildContextualTuples(tuples []contextualTupleCfg) ([]parsedContextualTuple, error) {
	if len(tuples) == 0 {
		return nil, nil
	}
	var errs []error
	parsed := make([]parsedContextualTuple, 0, len(tuples))
	for i := range tuples {
		ct := &tuples[i]
		var tupleErrs []error
		if err := validateValueSource(fmt.Sprintf("contextual_tuples[%d].user", i), &ct.User); err != nil {
			tupleErrs = append(tupleErrs, err)
		}
		if err := validateValueSource(fmt.Sprintf("contextual_tuples[%d].relation", i), &ct.Relation); err != nil {
			tupleErrs = append(tupleErrs, err)
		}
		if err := validateValueSource(fmt.Sprintf("contextual_tuples[%d].object", i), &ct.Object); err != nil {
			tupleErrs = append(tupleErrs, err)
		}
		if len(tupleErrs) > 0 {
			errs = append(errs, tupleErrs...)
			continue
		}
		ct.User.precompute()
		ct.Relation.precompute()
		ct.Object.precompute()
		parsed = append(parsed, parsedContextualTuple{
			user:     ct.User,
			relation: ct.Relation,
			object:   ct.Object,
		})
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}
	return parsed, nil
}

// buildContext validates and prepares context value sources for ABAC conditions.
func buildContext(ctx map[string]valueSource) (map[string]valueSource, error) {
	if len(ctx) == 0 {
		return nil, nil
	}
	var errs []error
	parsed := make(map[string]valueSource, len(ctx))
	for name, vs := range ctx {
		vsCopy := vs
		if err := validateValueSource(fmt.Sprintf("context[%s]", name), &vsCopy); err != nil {
			errs = append(errs, err)
			continue
		}
		vsCopy.precompute()
		parsed[name] = vsCopy
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}
	return parsed, nil
}

// buildCheckBody constructs the JSON body for the OpenFGA Check API call.
// Uses encoding/json to safely marshal values that may contain arbitrary characters.
// consistency must be empty or a value validated by validateConsistency; when empty, the field is omitted from JSON.
func buildCheckBody(user, relation, object, authorizationModelID, consistency string, contextualTuples []resolvedTuple, context map[string]string) ([]byte, error) {
	type tupleKey struct {
		User     string `json:"user"`
		Relation string `json:"relation"`
		Object   string `json:"object"`
	}
	type contextualTuplesWrapper struct {
		TupleKeys []tupleKey `json:"tuple_keys"`
	}
	type checkRequest struct {
		TupleKey             tupleKey                 `json:"tuple_key"`
		AuthorizationModelID string                   `json:"authorization_model_id,omitempty"`
		Consistency          string                   `json:"consistency,omitempty"`
		ContextualTuples     *contextualTuplesWrapper `json:"contextual_tuples,omitempty"`
		Context              map[string]string        `json:"context,omitempty"`
	}
	req := checkRequest{
		TupleKey:             tupleKey{User: user, Relation: relation, Object: object},
		AuthorizationModelID: authorizationModelID,
		Consistency:          consistency,
	}
	if len(contextualTuples) > 0 {
		keys := make([]tupleKey, len(contextualTuples))
		for i, ct := range contextualTuples {
			keys[i] = tupleKey(ct)
		}
		req.ContextualTuples = &contextualTuplesWrapper{TupleKeys: keys}
	}
	if len(context) > 0 {
		req.Context = context
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openfga: marshal Check request: %w", err)
	}
	return body, nil
}

// extractPathSegment returns the path segment at idx (0-based) from the URL path.
// Negative indices count from the end: -1 is the last segment.
// Returns "" if idx is out of range or the path is empty.
func extractPathSegment(fullPath string, idx int) string {
	pathPart, _, _ := strings.Cut(fullPath, "?")
	segments := strings.Split(strings.TrimPrefix(pathPart, "/"), "/")
	if idx < 0 {
		idx = len(segments) + idx
	}
	if idx < 0 || idx >= len(segments) {
		return ""
	}
	return segments[idx]
}

// extractQueryParam returns the first value of the named query parameter.
// Returns "" if the parameter is absent or the path has no query string.
func extractQueryParam(fullPath string, name string) string {
	_, queryPart, ok := strings.Cut(fullPath, "?")
	if !ok {
		return ""
	}
	vals, err := url.ParseQuery(queryPart)
	if err != nil {
		return ""
	}
	return vals.Get(name)
}
