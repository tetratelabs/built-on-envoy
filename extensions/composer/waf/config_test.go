// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/logger"
)

func Test_ParseConfig(t *testing.T) {
	marshal := func(t *testing.T, m map[string]any) []byte {
		t.Helper()
		b, err := json.Marshal(m)
		require.NoError(t, err)
		return b
	}

	t.Run("defaults", func(t *testing.T) {
		cfg, err := parseConfig(marshal(t, map[string]any{"directives": []string{"Include @coraza.conf", "SecRuleEngine On"}}), logger.GetLogger())
		require.NoError(t, err)
		require.Equal(t, "Include @coraza.conf\nSecRuleEngine On", cfg.engine.Directives)
		// REQUEST_ONLY is the default when no mode is set.
		require.Equal(t, modeRequestOnly, cfg.mode)
		require.Equal(t, defaultRequestURIPathOnly, cfg.requestURIPathOnly)
		require.Equal(t, metricsConfig{}, cfg.metrics)
	})

	t.Run("explicit mode", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			modeStr  string
			expected wafMode
		}{
			{"request_only", "REQUEST_ONLY", modeRequestOnly},
			{"full", "FULL", modeFull},
			// RESPONSE_ONLY is deprecated but must still parse for backward compatibility.
			{"response_only_deprecated", "RESPONSE_ONLY", modeResponseOnly},
		} {
			t.Run(tc.name, func(t *testing.T) {
				cfg, err := parseConfig(marshal(t, map[string]any{"directives": []string{"Include @coraza.conf"}, "mode": tc.modeStr}), logger.GetLogger())
				require.NoError(t, err)
				require.Equal(t, tc.expected, cfg.mode)
			})
		}
	})

	t.Run("request_uri_path_only", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			value    any
			expected bool
		}{
			{"unset", nil, defaultRequestURIPathOnly},
			{"true", true, true},
			{"false", false, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := map[string]any{"directives": []string{"Include @coraza.conf"}}
				if tc.value != nil {
					m["request_uri_path_only"] = tc.value
				}
				cfg, err := parseConfig(marshal(t, m), logger.GetLogger())
				require.NoError(t, err)
				require.Equal(t, tc.expected, cfg.requestURIPathOnly)
			})
		}
	})

	t.Run("one pass reads every setting", func(t *testing.T) {
		cfg, err := parseConfig(marshal(t, map[string]any{
			"directives":            []string{"Include @coraza.conf"},
			"mode":                  "FULL",
			"request_uri_path_only": true,
			"metrics":               map[string]any{"authority_label": false},
		}), logger.GetLogger())
		require.NoError(t, err)
		require.Equal(t, modeFull, cfg.mode)
		require.True(t, cfg.requestURIPathOnly)
		require.True(t, cfg.metrics.authority.disabled)
	})

	t.Run("invalid mode", func(t *testing.T) {
		_, err := parseConfig(marshal(t, map[string]any{"directives": []string{"Include @coraza.conf"}, "mode": "SIDEWAYS"}), logger.GetLogger())
		require.ErrorContains(t, err, "invalid mode")
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := parseConfig([]byte(`{invalid`), logger.GetLogger())
		require.ErrorContains(t, err, "failed to unmarshal config")
	})
}
