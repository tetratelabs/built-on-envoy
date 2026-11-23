// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These tests cover the shared test helpers.
package internaltesting

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireEventuallyRequestWithTiming_RetriesAssertions(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	RequireEventuallyRequestWithTiming(t, req, func(c *assert.CollectT, response *http.Response, duration time.Duration) bool {
		require.Equal(c, http.StatusOK, response.StatusCode)
		require.Positive(c, duration)
		return true
	})
	require.EqualValues(t, 2, attempts.Load())
}

func TestSkipIfEnvoyVersionUnsupported(t *testing.T) {
	// The composer/mcp-openapi manifest declares minEnvoyVersion 1.38.0.
	tests := []struct {
		name         string
		envoyVersion string
		constraints  EnvoyConstraints
		skipped      bool
	}{
		{"unset uses manifest minimum (unsupported)", "", EnvoyConstraints{MinEnvoyVersion: "1.40.0"}, true},
		{"unset uses manifest minimum (supported)", "", EnvoyConstraints{MinEnvoyVersion: "1.38.0"}, false},
		{"older than minimum", "1.39.4", EnvoyConstraints{MinEnvoyVersion: "1.40.0"}, true},
		{"equal to minimum", "1.40.0", EnvoyConstraints{MinEnvoyVersion: "1.40.0"}, false},
		{"newer than maximum", "1.40.0", EnvoyConstraints{MaxEnvoyVersion: "1.39.0"}, true},
		{"dev is always supported", "dev", EnvoyConstraints{MinEnvoyVersion: "99.0.0"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENVOY_VERSION", tt.envoyVersion)
			var skipped bool
			t.Run("check", func(t *testing.T) {
				defer func() { skipped = t.Skipped() }()
				SkipIfEnvoyVersionUnsupported(t, "composer/mcp-openapi", tt.constraints)
			})
			require.Equal(t, tt.skipped, skipped)
		})
	}
}
