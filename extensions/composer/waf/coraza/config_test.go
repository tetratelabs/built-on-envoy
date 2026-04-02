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
		require.Equal(t, ModeRequestOnly, mode)
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
