// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"io/fs"
	"testing"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/experimental"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/jcchavezs/mergefs/io"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNormalizeDirectivePath(t *testing.T) {
	for _, tc := range []struct {
		input, expected string
	}{
		{input: "@recommended.conf", expected: "@coraza.conf"},
		{input: "@recommended-conf", expected: "@coraza.conf"},
		{input: "@crs-setup-conf", expected: "@crs-setup.conf"},
		{input: "@coraza.conf", expected: "@coraza.conf"},
		{input: "@crs-setup.conf", expected: "@crs-setup.conf"},
		{input: "@ftw.conf", expected: "@ftw.conf"},
		{input: "@coraza.conf-recommended", expected: "@coraza.conf-recommended"},
		{input: "@crs-setup.conf.example", expected: "@crs-setup.conf.example"},
		{input: "@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf", expected: "@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf"},
		// Prefix stripping: Coraza prepends the including file's directory
		{input: "testdata/@coraza.conf", expected: "@coraza.conf"},
		{input: "testdata/@recommended.conf", expected: "@coraza.conf"},
		// No @ sign: pass-through unchanged
		{input: "filename.conf", expected: "filename.conf"},
		{input: "folder/filename.conf", expected: "folder/filename.conf"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			require.Equal(t, tc.expected, normalizeDirectivePath(tc.input))
		})
	}
}

func TestEmbeddedDirectives(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wantErr bool
	}{
		{name: "@coraza.conf"},
		{name: "@recommended.conf"}, // alias
		{name: "@crs-setup.conf"},
		{name: "@ftw.conf"},
		{name: "unknown", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("Open", func(t *testing.T) {
				file, err := embeddedConfigs.Open(tc.name)
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.NotNil(t, file)
					require.NoError(t, file.Close())
				}
			})
			t.Run("ReadFile", func(t *testing.T) {
				content, err := embeddedConfigs.ReadFile(tc.name)
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.NotEmpty(t, content)
				}
			})
		})
	}
}

// combinedDirectivesFS order is embedded > coreruleset > local FS (OSFS)
func TestCombinedDirectivesFS_ReadFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source fs.FS // the individual FS that should win for this file
	}{
		// Embedded directives take precedence for tailored configs
		{name: "@coraza.conf", source: embeddedConfigs},
		{name: "@crs-setup.conf", source: embeddedConfigs},
		{name: "@ftw.conf", source: embeddedConfigs},
		// Aliases resolve to their embedded canonical files
		{name: "@recommended.conf", source: embeddedConfigs},
		{name: "@recommended-conf", source: embeddedConfigs},
		{name: "@crs-setup-conf", source: embeddedConfigs},
		// Coreruleset FS serves upstream directives
		{name: "@coraza.conf-recommended", source: coreruleset.FS},
		{name: "@crs-setup.conf.example", source: coreruleset.FS},
		{name: "@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf", source: coreruleset.FS},
		// OSFS serves files from the local filesystem (testdata acts as a stand-in)
		{name: "testdata/custom-rule.conf", source: io.OSFS},
		{name: "testdata/custom-rule-2.conf", source: io.OSFS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			combined, err := fs.ReadFile(combinedDirectivesFS, tc.name)
			require.NoError(t, err)

			expected, err := fs.ReadFile(tc.source, tc.name)
			require.NoError(t, err)

			require.Equal(t, expected, combined)
		})
	}
}

func TestCombinedDirectivesFS_Glob(t *testing.T) {
	for _, tc := range []struct {
		pattern        string
		mustContain    []string
		mustNotContain []string
	}{
		{
			// Embedded files are not discoverable via generic glob patterns —
			// they must be accessed explicitly by @-prefixed name.
			pattern: "*.conf",
			mustNotContain: []string{
				"@coraza.conf",
				"@crs-setup.conf",
				"@ftw.conf",
			},
		},
		{
			// Coreruleset: owasp_crs directory
			pattern: "@owasp_crs/*.conf",
			mustContain: []string{
				"@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf",
				"@owasp_crs/REQUEST-942-APPLICATION-ATTACK-SQLI.conf",
			},
		},
		{
			// OSFS: local filesystem via testdata directory
			pattern: "testdata/*.conf",
			mustContain: []string{
				"testdata/custom-rule.conf",
				"testdata/custom-rule-2.conf",
			},
		},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			matches, err := fs.Glob(combinedDirectivesFS, tc.pattern)
			require.NoError(t, err)
			for _, m := range tc.mustContain {
				require.Contains(t, matches, m)
			}
			for _, m := range tc.mustNotContain {
				require.NotContains(t, matches, m)
			}
		})
	}
}

// Verifies that testdata/include-coraza.conf (loaded from OSFS) can include
// a special file name such as @coraza.conf (loaded from the embedded layer).
func TestCombinedDirectivesFS_EmbeddedDirectivesInNestedInclude(t *testing.T) {
	t.Run("baseline: ResponseBodyAccess is Off without @coraza.conf", func(t *testing.T) {
		waf, err := NewWAFFromDirectives("SecRuleEngine On", zap.NewNop())
		require.NoError(t, err)
		require.False(t, waf.NewTransaction().IsResponseBodyAccessible())
	})
	t.Run("embedded @coraza.conf resolves from within a local file include", func(t *testing.T) {
		waf, err := NewWAFFromDirectives("Include testdata/include-coraza.conf", zap.NewNop())
		require.NoError(t, err)
		require.True(t, waf.NewTransaction().IsResponseBodyAccessible())
	})
}

// Verifies that via testdata/include-coraza.conf (loaded from OSFS) is possible to
// include CRS rules (loaded from coreruleset FS)
func TestCombinedDirectivesFS_CRSRulesLoaded(t *testing.T) {
	var ruleIDs []int
	cfg := coraza.NewWAFConfig().
		WithErrorCallback(newSlogError(zap.NewNop())).
		WithRootFS(combinedDirectivesFS)
	cfg = experimental.WAFConfigWithRuleObserver(cfg, func(rule ctypes.RuleMetadata) {
		ruleIDs = append(ruleIDs, rule.ID())
	})
	cfg = cfg.WithDirectives("Include testdata/include-coraza.conf")
	_, err := coraza.NewWAF(cfg)
	require.NoError(t, err)
	require.Contains(t, ruleIDs, 900990) // from @crs-setup.conf.example
	require.Contains(t, ruleIDs, 949110) // from @owasp_crs/REQUEST-949-BLOCKING-EVALUATION.conf
	require.Contains(t, ruleIDs, 959060) // from @owasp_crs/RESPONSE-959-BLOCKING-EVALUATION.conf
}
