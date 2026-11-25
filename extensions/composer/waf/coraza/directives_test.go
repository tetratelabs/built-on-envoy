// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	corazaconf "github.com/corazawaf/coraza-coreruleset/coraza/v3"
	crs "github.com/corazawaf/coraza-coreruleset/crs/v4"
	lts "github.com/corazawaf/coraza-coreruleset/lts/v4"
	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/experimental"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/jcchavezs/mergefs/io"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/internal/crsregistry"
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
		// Prefix stripping when the including file's directory contains an @
		{input: "/etc/waf@prod/@coraza.conf", expected: "@coraza.conf"},
		{input: "/etc/waf@prod/@recommended.conf", expected: "@coraza.conf"},
		{input: "/go/pkg/mod/github.com/org/repo@v1.2.3/@ftw.conf", expected: "@ftw.conf"},
		// No @ sign: pass-through unchanged
		{input: "filename.conf", expected: "filename.conf"},
		{input: "folder/filename.conf", expected: "folder/filename.conf"},
		// @ sign not at the start of a path segment: pass-through unchanged
		{input: "/etc/waf@prod/custom.conf", expected: "/etc/waf@prod/custom.conf"},
		{input: "folder/foo@bar.conf", expected: "folder/foo@bar.conf"},
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
		{name: "@ftw.conf"},
		// The tailored CRS setup depends on the CRS version, see TestCRSBundles.
		{name: "@crs-setup.conf", wantErr: true},
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

// The CRS bundle FS order is embedded > embedded CRS setup > coreruleset modules (crs, corazaconf) > local FS (OSFS)
// The sources below are those of the crs/v4 module, so the test runs against its CRS version.
func TestCombinedDirectivesFS_ReadFile(t *testing.T) {
	latest, err := lookupCRSBundle("latest")
	require.NoError(t, err)
	require.Equal(t, strings.TrimPrefix(crs.Version, "v"), latest.version)
	setup := directivesFS{registeredCRS(t, latest.version).Setup}
	for _, tc := range []struct {
		name   string
		source fs.FS // the individual FS that should win for this file
	}{
		// Embedded directives take precedence for tailored configs
		{name: "@coraza.conf", source: embeddedConfigs},
		{name: "@crs-setup.conf", source: setup},
		{name: "@ftw.conf", source: embeddedConfigs},
		// Aliases resolve to their embedded canonical files
		{name: "@recommended.conf", source: embeddedConfigs},
		{name: "@recommended-conf", source: embeddedConfigs},
		{name: "@crs-setup-conf", source: setup},
		// Coreruleset modules serve upstream directives
		{name: "@coraza.conf-recommended", source: corazaconf.FS},
		{name: "@crs-setup.conf.example", source: crs.FS},
		{name: "@crs-setup.conf.example-nodefaultact", source: crs.FS},
		{name: "@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf", source: crs.FS},
		// OSFS serves files from the local filesystem (testdata acts as a stand-in)
		{name: "testdata/custom-rule.conf", source: io.OSFS},
		{name: "testdata/custom-rule-2.conf", source: io.OSFS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			combined, err := fs.ReadFile(latest.fs, tc.name)
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
			matches, err := fs.Glob(mustCRSBundles(t).defaultBundle.fs, tc.pattern)
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
		waf, err := newWAF("SecRuleEngine On", zap.NewNop(), mustCRSBundles(t).defaultBundle.fs)
		require.NoError(t, err)
		require.False(t, waf.NewTransaction().IsResponseBodyAccessible())
	})
	t.Run("embedded @coraza.conf resolves from within a local file include", func(t *testing.T) {
		waf, err := newWAF("Include testdata/include-coraza.conf", zap.NewNop(), mustCRSBundles(t).defaultBundle.fs)
		require.NoError(t, err)
		require.True(t, waf.NewTransaction().IsResponseBodyAccessible())
	})
	// The directory is generated at runtime to avoid committing an @ directory to testdata.
	t.Run("embedded @coraza.conf resolves from within a local file in a directory containing @", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "waf@prod")
		require.NoError(t, os.Mkdir(dir, 0o750))
		conf := filepath.Join(dir, "my.conf")
		require.NoError(t, os.WriteFile(conf, []byte("Include @coraza.conf\n"), 0o600))

		waf, err := newWAF("Include "+conf, zap.NewNop(), mustCRSBundles(t).defaultBundle.fs)
		require.NoError(t, err)
		require.True(t, waf.NewTransaction().IsResponseBodyAccessible())
	})
	// Same as above for files served by the coraza-coreruleset modules.
	for _, include := range []string{"@coraza.conf-recommended", "@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf"} {
		t.Run("upstream "+include+" resolves from within a local file in a directory containing @", func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "waf@prod")
			require.NoError(t, os.Mkdir(dir, 0o750))
			conf := filepath.Join(dir, "my.conf")
			require.NoError(t, os.WriteFile(conf, []byte("Include "+include+"\n"), 0o600))

			_, err := newWAF("Include "+conf, zap.NewNop(), mustCRSBundles(t).defaultBundle.fs)
			require.NoError(t, err)
		})
	}
}

// Verifies that via testdata/include-coraza.conf (loaded from OSFS) is possible to
// include CRS rules (loaded from coreruleset FS)
func TestCombinedDirectivesFS_CRSRulesLoaded(t *testing.T) {
	var ruleIDs []int
	cfg := coraza.NewWAFConfig().
		WithErrorCallback(newSlogError(zap.NewNop())).
		WithRootFS(mustCRSBundles(t).defaultBundle.fs)
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

// mustCRSBundles returns the registered CRS versions.
func mustCRSBundles(t *testing.T) *crsBundleSet {
	t.Helper()
	set, err := crsBundles()
	require.NoError(t, err)
	return set
}

// registeredCRS returns the registry entry of a CRS version.
func registeredCRS(t *testing.T, version string) crsregistry.Entry {
	t.Helper()
	snap, err := crsregistry.Global.Freeze()
	require.NoError(t, err)
	for _, e := range snap.Entries {
		if e.Version.Version == version {
			return e
		}
	}
	require.FailNow(t, "CRS version not registered", version)
	return crsregistry.Entry{}
}

func TestCRSBundles(t *testing.T) {
	for _, b := range mustCRSBundles(t).bundles {
		t.Run(b.version, func(t *testing.T) {
			for _, name := range []string{
				"@owasp_crs_lts/REQUEST-911-METHOD-ENFORCEMENT.conf",
				"@crs-setup-lts.conf.example",
			} {
				t.Run("hides "+name, func(t *testing.T) {
					_, err := fs.ReadFile(b.fs, name)
					require.ErrorIs(t, err, fs.ErrNotExist)
				})
			}

			for _, setup := range []string{"@crs-setup.conf", "@crs-setup.conf.example"} {
				t.Run("rules are CRS "+b.version+" with "+setup, func(t *testing.T) {
					var versions []string
					cfg := coraza.NewWAFConfig().
						WithRootFS(b.fs).
						WithErrorCallback(func(mr ctypes.MatchedRule) { versions = append(versions, mr.Rule().Version()) }).
						WithDirectives(strings.Join([]string{
							"Include @coraza.conf",
							"SecRuleEngine On",
							"Include " + setup,
							"Include @owasp_crs/*.conf",
						}, "\n"))
					w, err := coraza.NewWAF(cfg)
					require.NoError(t, err)

					tx := w.NewTransaction()
					tx.ProcessURI("/?x=<script>alert(1)</script>", "GET", "HTTP/1.1")
					tx.AddRequestHeader("Host", "example.com")
					tx.ProcessRequestHeaders()
					_, err = tx.ProcessRequestBody()
					require.NoError(t, err)
					require.NotNil(t, tx.Interruption())
					require.NoError(t, tx.Close())

					require.NotEmpty(t, versions)
					for _, v := range versions {
						require.Equal(t, "OWASP_CRS/"+b.version, v)
					}
				})
			}
		})
	}
}

// The lts/v4 module is served under the crs/v4 names.
func TestLTSAliasFS(t *testing.T) {
	ltsBundle, err := lookupCRSBundle("lts")
	require.NoError(t, err)
	require.Equal(t, strings.TrimPrefix(lts.Version, "v"), ltsBundle.version)
	for name, ltsName := range map[string]string{
		"@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf": "@owasp_crs_lts/REQUEST-911-METHOD-ENFORCEMENT.conf",
		"@owasp_crs/unix-shell.data":                     "@owasp_crs_lts/unix-shell.data",
		"@crs-setup.conf.example":                        "@crs-setup-lts.conf.example",
		"@crs-setup.conf.example-nodefaultact":           "@crs-setup-lts.conf.example-nodefaultact",
		// A nested include, prefixed by the directory of the including file.
		"/etc/waf@prod/@owasp_crs/REQUEST-911-METHOD-ENFORCEMENT.conf": "@owasp_crs_lts/REQUEST-911-METHOD-ENFORCEMENT.conf",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := fs.ReadFile(ltsBundle.fs, name)
			require.NoError(t, err)
			want, err := fs.ReadFile(lts.FS, ltsName)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

// Globs and listings show the same names for every CRS version, never the lts/v4 module's own.
func TestCRSRulesFSNames(t *testing.T) {
	forEachCRSVersion(t, func(t *testing.T, root fs.FS) {
		rules, err := fs.Glob(root, "@owasp_crs/*.conf")
		require.NoError(t, err)
		require.NotEmpty(t, rules)
		for pattern, want := range map[string][]string{
			"@owasp_cr?/*.conf":        rules,
			"@crs-setup.conf.example*": {"@crs-setup.conf.example", "@crs-setup.conf.example-nodefaultact"},
			"@owasp_crs":               {"@owasp_crs"},
			// A directory before the name is ignored, as the upstream modules do.
			"/etc/waf/@owasp_crs/*.conf":        rules,
			"/etc/waf/@crs-setup.conf.example*": {"@crs-setup.conf.example", "@crs-setup.conf.example-nodefaultact"},
		} {
			got, globErr := fs.Glob(root, pattern)
			require.NoError(t, globErr, pattern)
			require.Equal(t, want, got, pattern)
			for _, name := range got {
				_, statErr := fs.Stat(root, name)
				require.NoError(t, statErr, name)
			}
		}
		top, err := fs.Glob(root, "@*")
		require.NoError(t, err)
		for _, name := range top {
			require.NotContains(t, name, "lts", "hidden lts/v4 name listed")
		}
		// The listing the merged filesystem serves, as globs and Coraza see it.
		entries, err := fs.ReadDir(root, ".")
		require.NoError(t, err)
		var names []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "@") { // the rest is the local filesystem
				names = append(names, e.Name())
			}
		}
		require.Contains(t, names, "@owasp_crs")
		require.Contains(t, names, "@crs-setup.conf.example")
		for _, name := range names {
			require.NotContains(t, name, "lts", "hidden lts/v4 name listed")
		}
		// A glob under the lts/v4 names fails instead of matching nothing.
		for _, pattern := range []string{"@owasp_crs_lts/*.conf", "@crs-setup-lts.conf.*"} {
			_, globErr := fs.Glob(root, pattern)
			require.EqualError(t, globErr, fmt.Sprintf("%q: @owasp_crs_lts/ and @crs-setup-lts.conf.example* are not served; "+
				"use @owasp_crs/ and @crs-setup.conf.example for every crs_version", pattern))
		}
		// So does a configuration that includes them.
		_, wafErr := newWAF("Include @crs-setup.conf\nInclude @owasp_crs_lts/*.conf", zap.NewNop(), root)
		require.ErrorContains(t, wafErr, "are not served")
		// A local directory of that name is still included by its path.
		local := filepath.Join(t.TempDir(), "@owasp_crs_lts")
		require.NoError(t, os.Mkdir(local, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(local, "local.conf"), []byte("SecAction \"id:12345,phase:1,pass,nolog\"\n"), 0o600))
		matches, globErr := fs.Glob(root, filepath.Join(local, "*.conf"))
		require.NoError(t, globErr)
		require.Equal(t, []string{filepath.Join(local, "local.conf")}, matches)
	})
}

// The config schema accepts exactly the aliases: this extension does not select its versions by
// their exact version.
func TestConfigSchemaCRSVersions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "config.schema.json"))
	require.NoError(t, err)
	var schema struct {
		Properties struct {
			CRSVersion struct {
				Enum []string `json:"enum"`
			} `json:"crs_version"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	require.ElementsMatch(t, slices.Collect(maps.Keys(mustCRSBundles(t).aliases)), schema.Properties.CRSVersion.Enum)
}

// crs_version selects a version only through its aliases, its exact version included.
func TestCRSBundleSetLookup(t *testing.T) {
	exposed := &crsBundle{version: "4.25.1"}
	hidden := &crsBundle{version: "4.25.2"}
	latest := &crsBundle{version: "4.30.0"}
	set := &crsBundleSet{
		bundles:       []*crsBundle{latest, hidden, exposed},
		defaultBundle: exposed,
		aliases:       map[string]*crsBundle{"lts": hidden, "latest": latest, "4.25.1": exposed},
	}
	for version, want := range map[string]*crsBundle{"": exposed, "4.25.1": exposed, "lts": hidden, "latest": latest} {
		got, err := set.lookup(version)
		require.NoError(t, err, version)
		require.Same(t, want, got, version)
	}
	_, err := set.lookup("4.25.2")
	require.EqualError(t, err, `unsupported crs_version "4.25.2", supported values are: 4.25.1, latest (4.30.0), lts (4.25.2); CRS 4.25.2 is selected by "lts"`)
	_, err = set.lookup("9.9.9")
	require.EqualError(t, err, `unsupported crs_version "9.9.9", supported values are: 4.25.1, latest (4.30.0), lts (4.25.2)`)
}

// A registered CRS version is served under the same names as
// the default ones.
func TestRegisteredCRSVersion(t *testing.T) {
	const version = "9.9.9"
	rules := `SecAction "id:901001,phase:1,pass,nolog,ver:'OWASP_CRS/` + version + `'"` + "\n"
	r := &crsregistry.Registry{}
	require.NoError(t, r.Register(crsregistry.Version{
		Version: version,
		Rules: fstest.MapFS{
			"@owasp_crs/REQUEST-901-INITIALIZATION.conf": {Data: []byte(rules)},
			"@crs-setup.conf.example":                    {Data: []byte("# OWASP CRS ver." + version + "\n")},
		},
		Setup: fstest.MapFS{"@crs-setup.conf": {Data: []byte("# OWASP CRS ver." + version + "\n" +
			`SecAction "id:900990,phase:1,pass,nolog,setvar:tx.crs_setup_version=999"` + "\n")}},
	}))
	require.NoError(t, r.SetDefault(version))
	snap, err := r.Freeze()
	require.NoError(t, err)
	b := newCRSBundle(snap.Entries[0])

	var ruleIDs []int
	cfg := coraza.NewWAFConfig().WithRootFS(b.fs).WithDirectives(strings.Join([]string{
		"Include @crs-setup.conf",
		"Include @owasp_crs/*.conf",
	}, "\n"))
	cfg = experimental.WAFConfigWithRuleObserver(cfg, func(rule ctypes.RuleMetadata) { ruleIDs = append(ruleIDs, rule.ID()) })
	_, err = coraza.NewWAF(cfg)
	require.NoError(t, err)
	require.Equal(t, []int{900990, 901001}, ruleIDs)

	// Coraza resolves an Include in a local file against that file's directory: the rules must
	// be found under that prefix, which the upstream modules strip themselves but a registered
	// fs.FS may not.
	local := filepath.Join(t.TempDir(), "main.conf")
	require.NoError(t, os.WriteFile(local, []byte("Include @owasp_crs/REQUEST-901-INITIALIZATION.conf\n"), 0o600))
	_, err = coraza.NewWAF(coraza.NewWAFConfig().WithRootFS(b.fs).WithDirectives("Include " + local))
	require.NoError(t, err)
}

func TestCompareVersions(t *testing.T) {
	require.Equal(t, -1, compareVersions("4.25.2", "4.30.0"))
	require.Equal(t, -1, compareVersions("4.25.2", "4.25.10"))
	require.Equal(t, 1, compareVersions("5.0.0", "4.30.0"))
	require.Equal(t, 0, compareVersions("4.25.1", "4.25.1"))
}
