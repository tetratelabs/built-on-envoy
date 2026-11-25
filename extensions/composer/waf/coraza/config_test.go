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
		// Each CRS version keeps its own WAF: building one doesn't evict the other's.
		require.Same(t, latest, get(t, "latest"))
		require.Same(t, lts, get(t, "lts"))
	})

	t.Run("invalid crs_version", func(t *testing.T) {
		// Only the aliases are accepted: the embedded versions are not exposed.
		latest, err := lookupCRSBundle("latest")
		require.NoError(t, err)
		lts, err := lookupCRSBundle("lts")
		require.NoError(t, err)
		supported := fmt.Sprintf("supported values are: latest (%s), lts (%s)", latest.version, lts.version)
		for v, want := range map[string]string{
			latest.version: fmt.Sprintf(`unsupported crs_version %q, %s; CRS %s is selected by "latest"`, latest.version, supported, latest.version),
			"stable":       fmt.Sprintf(`unsupported crs_version "stable", %s`, supported),
			"LTS":          fmt.Sprintf(`unsupported crs_version "LTS", %s`, supported),
		} {
			t.Run(v, func(t *testing.T) {
				waf, err := GetOrCreateSharedWAFFromConfig(Config{Directives: "Include @coraza.conf", CRSVersion: v}, logger.GetLogger())
				require.EqualError(t, err, want)
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
