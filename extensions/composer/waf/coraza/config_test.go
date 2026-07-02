// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/logger"
)

func Test_newWAF(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		config := map[string]interface{}{
			"directives": []string{
				"Include @coraza.conf",
				"Include @ftw.conf",
				"Include @crs-setup.conf",
				"Include @owasp_crs/*.conf",
			},
		}

		// Convert map to JSON bytes
		wafConfig, _ := json.Marshal(config)

		waf, mode, err := NewWAFConfigFromBytes(wafConfig, logger.GetLogger())
		require.NoError(t, err)
		require.NotNil(t, waf)
		// REQUEST_ONLY is the default when no mode is set.
		require.Equal(t, ModeRequestOnly, mode)
	})

	t.Run("explicit mode", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			modeStr  string
			expected WAFMode
		}{
			{"request_only", "REQUEST_ONLY", ModeRequestOnly},
			{"full", "FULL", ModeFull},
			// RESPONSE_ONLY is deprecated but must still parse for backward compatibility.
			{"response_only_deprecated", "RESPONSE_ONLY", ModeResponseOnly},
		} {
			t.Run(tc.name, func(t *testing.T) {
				config := map[string]interface{}{
					"directives": []string{
						"Include @coraza.conf",
						"Include @ftw.conf",
						"Include @crs-setup.conf",
						"Include @owasp_crs/*.conf",
					},
					"mode": tc.modeStr,
				}
				wafConfig, _ := json.Marshal(config)

				waf, mode, err := NewWAFConfigFromBytes(wafConfig, logger.GetLogger())
				require.NoError(t, err)
				require.NotNil(t, waf)
				require.Equal(t, tc.expected, mode)
			})
		}
	})

	t.Run("invalid mode", func(t *testing.T) {
		config := map[string]interface{}{
			"directives": []string{"Include @coraza.conf"},
			"mode":       "SIDEWAYS",
		}
		wafConfig, _ := json.Marshal(config)

		waf, _, err := NewWAFConfigFromBytes(wafConfig, logger.GetLogger())
		require.ErrorContains(t, err, "invalid mode")
		require.Nil(t, waf)
	})

	t.Run("error", func(t *testing.T) {
		config := map[string]interface{}{
			"directives": []string{
				"foo",
			},
		}
		// Convert map to JSON bytes
		wafConfig, _ := json.Marshal(config)

		waf, _, err := NewWAFConfigFromBytes(wafConfig, logger.GetLogger())
		require.ErrorContains(t, err, "failed to create WAF")
		require.Nil(t, waf)
	})
}
