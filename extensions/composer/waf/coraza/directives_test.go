// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"io/fs"
	"testing"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
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

// CombinedDirectivesFS order is embedded > coreruleset > local FS (OSFS)
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
			combined, err := fs.ReadFile(CombinedDirectivesFS, tc.name)
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
			// Embedded files are not discoverable via generic glob patterns
			// They must be accessed explicitly by @-prefixed name.
			// *.conf will match OSFS files.
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
			matches, err := fs.Glob(CombinedDirectivesFS, tc.pattern)
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

func TestCombinedDirectivesFS_EmbeddedDirectivesInNestedInclude(t *testing.T) {
	waf, err := NewWAFFromDirectives("SecRuleEngine On", zap.NewNop())
	require.NoError(t, err)
	require.False(t, waf.NewTransaction().IsResponseBodyAccessible(), "ResponseBodyAccess should be Off by default")
	// testdata/include-coraza.conf contains "Include @coraza.conf".
	// Despite being included from a local path, the embedded @coraza.conf should be correctly resolved and loaded by the WAF.
	waf, err = NewWAFFromDirectives("Include testdata/include-coraza.conf", zap.NewNop())
	require.NoError(t, err)
	require.True(t, waf.NewTransaction().IsResponseBodyAccessible(), "ResponseBodyAccess should be enabled by @coraza.conf included via testdata/include-coraza.conf")
}
