// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package openfga implements an OpenFGA authorization HTTP filter plugin.
package openfga

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const defaultTimeoutMs uint64 = 5000

// openfgaConfig holds the JSON configuration for this filter.
type openfgaConfig struct {
	Cluster              string      `json:"cluster"`
	OpenFGAHost          string      `json:"openfga_host"`
	StoreID              string      `json:"store_id"`
	AuthorizationModelID string      `json:"authorization_model_id"`
	User                 valueSource `json:"user"`
	Relation             valueSource `json:"relation"`
	Object               valueSource `json:"object"`
	Rules                []checkRule `json:"rules"`
	FailOpen             bool        `json:"fail_open"`
	DryRun               bool        `json:"dry_run"`
	TimeoutMs            uint64      `json:"timeout_ms"`
	DenyStatus           int              `json:"deny_status"`
	DenyBody             string           `json:"deny_body"`
	Metadata             *pkg.MetadataKey `json:"metadata,omitempty"`
}

// valueSource defines how to extract a value from the request.
// Exactly one of Value or Header must be set. Prefix is optional and prepended to the result.
type valueSource struct {
	Value    string `json:"value"`
	Header   string `json:"header"`
	Prefix   string `json:"prefix"`
	resolved string // set at config time when Value is static; used by resolve to avoid per-request work
}

func (v *valueSource) isEmpty() bool {
	return v.Value == "" && v.Header == ""
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
	if v.Value != "" {
		raw = v.Value
	} else if v.Header != "" {
		raw = headers.GetOne(v.Header)
	}
	if raw == "" {
		return ""
	}
	return v.Prefix + raw
}

// ruleMatch defines conditions that must all be true for a rule to apply.
// "*" means the header must be present with any non-empty value; any other
// string requires an exact match.
type ruleMatch struct {
	Headers map[string]string `json:"headers"`
}

func (m *ruleMatch) matches(headers shared.HeaderMap) bool {
	for name, want := range m.Headers {
		got := headers.GetOne(name)
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
	if len(errs) > 0 {
		return nil, fmt.Errorf("openfga: %w", errors.Join(errs...))
	}

	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = defaultTimeoutMs
	}

	denyStatus := cfg.DenyStatus
	if denyStatus == 0 {
		denyStatus = 403
	}
	if denyStatus < 100 || denyStatus > 599 {
		return nil, fmt.Errorf("openfga: deny_status must be between 100 and 599, got %d", denyStatus)
	}
	denyBody := cfg.DenyBody
	if denyBody == "" {
		denyBody = "Forbidden"
	}
	deny := pkg.LocalResponse{Status: denyStatus, Body: denyBody}
	if err := deny.Validate(); err != nil {
		return nil, fmt.Errorf("openfga: %w", err)
	}
	denyHeaders := buildDenyHeaders(deny)

	if cfg.Metadata != nil {
		if err := cfg.Metadata.Validate(); err != nil {
			return nil, fmt.Errorf("openfga: invalid metadata config: %w", err)
		}
	}

	rules, err := buildRules(cfg)
	if err != nil {
		return nil, err
	}

	checkPath := "/stores/" + cfg.StoreID + "/check"

	return &parsedConfig{
		cluster:              cfg.Cluster,
		authorizationModelID: cfg.AuthorizationModelID,
		failOpen:             cfg.FailOpen,
		dryRun:               cfg.DryRun,
		timeoutMs:            cfg.TimeoutMs,
		deny:                 deny,
		denyHeaders:          denyHeaders,
		denyBodyBytes:        []byte(denyBody),
		checkPath:            checkPath,
		metadata:             cfg.Metadata,
		calloutHeaders: [][2]string{
			{":method", "POST"},
			{":path", checkPath},
			{"host", cfg.OpenFGAHost},
			{"content-type", "application/json"},
		},
		rules: rules,
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
	for i, cr := range cfg.Rules {
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
	if vs.Value == "" && vs.Header == "" {
		return fmt.Errorf("%s: one of 'value' or 'header' must be set", name)
	}
	if vs.Value != "" && vs.Header != "" {
		return fmt.Errorf("%s: only one of 'value' or 'header' may be set", name)
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

// sendDeny sends a local response using the configured deny status, body, and headers.
func (c *parsedConfig) sendDeny(handle shared.HttpFilterHandle, grpcStatus string) {
	handle.SendLocalResponse(uint32(c.deny.Status), c.denyHeaders, c.denyBodyBytes, grpcStatus)
}

// writeMetadata writes the authorization decision to dynamic metadata if configured.
func (c *parsedConfig) writeMetadata(handle shared.HttpFilterHandle, decision string) {
	if c.metadata != nil {
		handle.SetMetadata(c.metadata.Namespace, c.metadata.Key, decision)
	}
}

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// buildCheckBody constructs the JSON body for the OpenFGA Check API call.
// Values come from trusted sources (static config or Envoy request headers) and
// are assumed not to contain characters requiring JSON escaping (", \, control chars).
func buildCheckBody(user, relation, object, authorizationModelID string) []byte {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(`{"tuple_key":{"user":"`)
	buf.WriteString(user)
	buf.WriteString(`","relation":"`)
	buf.WriteString(relation)
	buf.WriteString(`","object":"`)
	buf.WriteString(object)
	buf.WriteString(`"}`)
	if authorizationModelID != "" {
		buf.WriteString(`,"authorization_model_id":"`)
		buf.WriteString(authorizationModelID)
		buf.WriteString(`"`)
	}
	buf.WriteByte('}')
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	bufPool.Put(buf)
	return out
}
