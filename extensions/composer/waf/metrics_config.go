// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// labelSource is where the value of the authority tag comes from.
type labelSource int

const (
	// labelSourceAuthority is the request :authority header without its port.
	labelSourceAuthority labelSource = iota
	// labelSourceRouteName is the name of the route that matched the request.
	labelSourceRouteName
	// labelSourceVirtualHostName is the name of the virtual host that matched the request.
	labelSourceVirtualHostName
)

// authorityLabels maps the values of metrics.authority_label to the authority tag they configure.
var authorityLabels = map[string]authorityLabel{
	"authority":         {source: labelSourceAuthority},
	"route_name":        {source: labelSourceRouteName},
	"virtual_host_name": {source: labelSourceVirtualHostName},
	"none":              {disabled: true},
}

// unresolvedLabel is the value of the authority tag when the label source has no value for the
// request: a route or virtual host without a name, or an Envoy version that does not expose the
// attribute to the filter.
const unresolvedLabel = "unresolved"

// authorityLabel decides the authority tag of the WAF metrics.
type authorityLabel struct {
	// disabled drops the authority tag: the metrics are defined and recorded without it.
	disabled bool
	// source is where the value of the tag comes from.
	source labelSource
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
func (a authorityLabel) tags(h shared.HttpFilterHandle, requestAuthority string, rest ...string) []string {
	if a.disabled {
		return rest
	}
	return append([]string{a.value(h, requestAuthority)}, rest...)
}

// value is the value of the authority tag for the request.
func (a authorityLabel) value(h shared.HttpFilterHandle, requestAuthority string) string {
	var (
		b  shared.UnsafeEnvoyBuffer
		ok bool
	)
	switch a.source {
	case labelSourceRouteName:
		b, ok = h.GetAttributeString(shared.AttributeIDXdsRouteName)
	case labelSourceVirtualHostName:
		b, ok = h.GetAttributeString(shared.AttributeIDXdsVirtualHostName)
	default:
		return requestAuthority
	}
	if !ok {
		return unresolvedLabel
	}
	if v := b.ToString(); v != "" {
		return v
	}
	return unresolvedLabel
}

// metricsConfig holds the metrics settings of a filter configuration. Its zero value is the default.
//
// Metrics are defined once per filter configuration, so these settings are read from the filter
// configuration only: per-route configurations do not override them. It is parsed by parseConfig.
type metricsConfig struct {
	authority authorityLabel
}
