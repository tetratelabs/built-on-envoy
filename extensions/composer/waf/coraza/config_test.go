// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/logger"
)

func TestGetOrCreateSharedWAFFromConfig(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cfg := Config{Directives: "Include @coraza.conf\nInclude @ftw.conf\nInclude @crs-setup.conf\nInclude @owasp_crs/*.conf"}

		waf, err := GetOrCreateSharedWAFFromConfig(cfg, logger.GetLogger())
		require.NoError(t, err)
		require.NotNil(t, waf)
	})

	t.Run("shared by directives", func(t *testing.T) {
		cfg := Config{Directives: "Include @coraza.conf"}

		first, err := GetOrCreateSharedWAFFromConfig(cfg, logger.GetLogger())
		require.NoError(t, err)
		second, err := GetOrCreateSharedWAFFromConfig(cfg, logger.GetLogger())
		require.NoError(t, err)
		require.Same(t, first, second)
	})

	t.Run("error", func(t *testing.T) {
		waf, err := GetOrCreateSharedWAFFromConfig(Config{Directives: "foo"}, logger.GetLogger())
		require.ErrorContains(t, err, "failed to create WAF")
		require.Nil(t, waf)
	})
}
