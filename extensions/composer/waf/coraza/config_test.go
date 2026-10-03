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

func TestParseConfigAndGetOrCreateSharedWAF(t *testing.T) {
	directives := []string{
		"Include @coraza.conf",
		"Include @ftw.conf",
		"Include @crs-setup.conf",
		"Include @owasp_crs/*.conf",
	}

	t.Run("default mode", func(t *testing.T) {
		config := marshalConfig(t, directives, "")
		parsed, err := ParseConfig(config, logger.GetLogger())
		require.NoError(t, err)
		require.Equal(t, ModeRequestOnly, parsed.Mode)
		require.Equal(t, "Include @coraza.conf\nInclude @ftw.conf\nInclude @crs-setup.conf\nInclude @owasp_crs/*.conf", parsed.Directives)

		waf, err := GetOrCreateSharedWAF(parsed.Directives, logger.GetLogger())
		require.NoError(t, err)
		require.NotNil(t, waf)
	})

	t.Run("explicit modes", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			mode     string
			expected WAFMode
		}{
			{name: "request_only", mode: "REQUEST_ONLY", expected: ModeRequestOnly},
			{name: "full", mode: "FULL", expected: ModeFull},
			{name: "response_only_deprecated", mode: "RESPONSE_ONLY", expected: ModeResponseOnly},
		} {
			t.Run(tc.name, func(t *testing.T) {
				parsed, err := ParseConfig(marshalConfig(t, directives, tc.mode), logger.GetLogger())
				require.NoError(t, err)
				require.Equal(t, tc.expected, parsed.Mode)

				waf, err := GetOrCreateSharedWAF(parsed.Directives, logger.GetLogger())
				require.NoError(t, err)
				require.NotNil(t, waf)
			})
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		parsed, err := ParseConfig([]byte("{"), logger.GetLogger())
		require.ErrorContains(t, err, "failed to unmarshal config")
		require.Equal(t, Config{}, parsed)
	})

	t.Run("invalid mode", func(t *testing.T) {
		parsed, err := ParseConfig(marshalConfig(t, []string{"Include @coraza.conf"}, "SIDEWAYS"), logger.GetLogger())
		require.ErrorContains(t, err, "invalid mode")
		require.Equal(t, Config{}, parsed)
	})

	t.Run("invalid directives", func(t *testing.T) {
		parsed, err := ParseConfig(marshalConfig(t, []string{"foo"}, ""), logger.GetLogger())
		require.NoError(t, err)
		waf, err := GetOrCreateSharedWAF(parsed.Directives, logger.GetLogger())
		require.ErrorContains(t, err, "failed to create WAF from directives")
		require.Nil(t, waf)
	})
}

func marshalConfig(t *testing.T, directives []string, mode string) []byte {
	t.Helper()
	config := struct {
		Directives []string `json:"directives"`
		Mode       string   `json:"mode,omitempty"`
	}{Directives: directives, Mode: mode}

	data, err := json.Marshal(config)
	require.NoError(t, err)
	return data
}
