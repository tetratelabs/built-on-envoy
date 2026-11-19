// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"fmt"
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

	t.Run("crs_version", func(t *testing.T) {
		directives := "Include @coraza.conf\nInclude @crs-setup.conf\nInclude @owasp_crs/*.conf"
		get := func(t *testing.T, crsVersion string) *SharedWAF {
			t.Helper()
			waf, err := GetOrCreateSharedWAFFromConfig(Config{Directives: directives, CRSVersion: crsVersion}, logger.GetLogger())
			require.NoError(t, err)
			return waf
		}

		// Keep the WAFs reachable, so that the shared ones are not collected mid-test.
		defaultWAF, lts, latest := get(t, ""), get(t, "lts"), get(t, "latest")
		// The default is the LTS release.
		require.Same(t, defaultWAF, lts)
		// Same directives against another CRS version must not share the WAF.
		require.NotSame(t, lts, latest)
		require.Same(t, latest, get(t, "latest"))
	})

	t.Run("invalid crs_version", func(t *testing.T) {
		// Only the aliases are accepted: the embedded versions are not exposed.
		for _, v := range []string{"4.30.0", "4.25.2", "4", "4.25", "4.30", "4.26.0", "9.9.9", "v4.30.0", "LTS", "stable"} {
			t.Run(v, func(t *testing.T) {
				waf, err := GetOrCreateSharedWAFFromConfig(Config{Directives: "Include @coraza.conf", CRSVersion: v}, logger.GetLogger())
				require.EqualError(t, err, fmt.Sprintf("unsupported crs_version %q, supported values are: latest (4.30.0), lts (4.25.2)", v))
				require.Nil(t, waf)
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		waf, err := GetOrCreateSharedWAFFromConfig(Config{Directives: "foo"}, logger.GetLogger())
		require.ErrorContains(t, err, "failed to create WAF")
		require.Nil(t, waf)
	})
}
