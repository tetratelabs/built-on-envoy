// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

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
// configuration only: per-route configurations do not override them. It is parsed by parseConfig.
type metricsConfig struct {
	authority authorityLabel
}
