// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// authorityLabel decides the authority tag of the WAF metrics.
type authorityLabel struct {
	// disabled drops the authority tag: the metrics are defined and recorded without it.
	disabled bool
}

// tagNames returns the tag names of a metric tagged with the authority, unless the authority tag
// is disabled, followed by the given tag names.
func (a authorityLabel) tagNames(rest ...string) []string {
	if a.disabled {
		return rest
	}
	return append([]string{"authority"}, rest...)
}

// tags returns the tag values of a metric defined with tagNames, for a request whose :authority
// header, without its port, is requestAuthority, followed by the given tag values.
func (a authorityLabel) tags(requestAuthority string, rest ...string) []string {
	if a.disabled {
		return rest
	}
	return append([]string{requestAuthority}, rest...)
}

// metricsConfig holds the metrics settings of a filter configuration. Its zero value is the default.
//
// Metrics are defined once per filter configuration, so these settings are read from the filter
// configuration only: per-route configurations do not override them.
type metricsConfig struct {
	authority authorityLabel
}

// parseMetricsConfig parses the metrics object of the raw filter configuration. Unknown keys in it
// are rejected, so that a misspelled setting is not silently ignored.
func parseMetricsConfig(configBytes []byte) (metricsConfig, error) {
	var y struct {
		Metrics json.RawMessage `json:"metrics"`
	}
	if err := json.Unmarshal(configBytes, &y); err != nil {
		return metricsConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	var cfg metricsConfig
	if len(y.Metrics) == 0 {
		return cfg, nil
	}
	var m struct {
		AuthorityLabel *bool `json:"authority_label"`
	}
	dec := json.NewDecoder(bytes.NewReader(y.Metrics))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return metricsConfig{}, fmt.Errorf("invalid metrics: %w", err)
	}

	if m.AuthorityLabel != nil && !*m.AuthorityLabel {
		cfg.authority.disabled = true
	}
	return cfg, nil
}
