// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import (
	"strings"
)

// MatchConfig defines how a request is matched to an endpoint.
type MatchConfig struct {
	Prefix  string              `yaml:"prefix,omitempty"`
	Exact   string              `yaml:"exact,omitempty"`
	Headers []HeaderMatchConfig `yaml:"headers,omitempty"`
}

// HeaderMatchConfig defines a header-based match condition.
type HeaderMatchConfig struct {
	Name         string `yaml:"name"`
	ExactMatch   string `yaml:"exact_match,omitempty"`
	PresentMatch bool   `yaml:"present_match,omitempty"`
}

// HeaderGetter provides read access to headers.
type HeaderGetter interface {
	GetOne(name string) string
}

// MatchRoute checks if a request matches the route's match configuration.
func MatchRoute(match MatchConfig, path string, headers HeaderGetter) bool {
	// Check path matching.
	if match.Prefix != "" {
		if !strings.HasPrefix(path, match.Prefix) {
			return false
		}
	}
	if match.Exact != "" {
		// Strip query string for exact matching.
		requestPath := path
		if idx := strings.Index(requestPath, "?"); idx != -1 {
			requestPath = requestPath[:idx]
		}
		if requestPath != match.Exact {
			return false
		}
	}

	// Check header matching.
	for _, hm := range match.Headers {
		value := headers.GetOne(hm.Name)
		if hm.PresentMatch {
			if value == "" {
				return false
			}
		} else if hm.ExactMatch != "" {
			if value != hm.ExactMatch {
				return false
			}
		}
	}

	return true
}

// ShouldApply returns true if the given percentage check passes.
func ShouldApply(percentage float64) bool {
	if percentage >= 100 {
		return true
	}
	if percentage <= 0 {
		return false
	}
	// Use crypto/rand for unbiased sampling.
	return cryptoFloat64()*100 < percentage
}
