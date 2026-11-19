// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package crs

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	crslatest "github.com/corazawaf/coraza-coreruleset/crs/v4"
	crslts "github.com/corazawaf/coraza-coreruleset/lts/v4"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/internal/crsregistry"
)

// The newest CRS release and the CRS LTS release are registered, selected only by their aliases,
// and the LTS release is the default.
func TestBuiltinVersions(t *testing.T) {
	snap, err := crsregistry.Global.Freeze()
	require.NoError(t, err)
	entries := snap.Entries

	latest, lts := strings.TrimPrefix(crslatest.Version, "v"), strings.TrimPrefix(crslts.Version, "v")
	require.Equal(t, latest, LatestVersion)
	require.Equal(t, lts, LTSVersion)
	require.Equal(t, lts, snap.Default)
	require.Equal(t, map[string]string{AliasLatest: latest, AliasLTS: lts}, snap.Aliases)
	require.Len(t, entries, 2)
	require.Equal(t, latest, entries[0].Version.Version)
	require.False(t, entries[0].LTSNames)
	require.Equal(t, lts, entries[1].Version.Version)
	require.True(t, entries[1].LTSNames)
}

// Every embedded setup is registered: a leftover setup of a former CRS version fails here.
func TestEmbeddedSetups(t *testing.T) {
	dirs, err := fs.ReadDir(setupEmbed, "setup")
	require.NoError(t, err)
	var versions []string
	for _, d := range dirs {
		versions = append(versions, strings.TrimPrefix(d.Name(), "v"))
	}
	require.ElementsMatch(t, []string{
		strings.TrimPrefix(crslatest.Version, "v"),
		strings.TrimPrefix(crslts.Version, "v"),
	}, versions)
}

// activeDirectives returns the lines of a configuration file that are not comments.
func activeDirectives(conf string) string {
	var lines []string
	for _, l := range strings.Split(conf, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// Each tailored setup is a hand-maintained file that may differ from its upstream example, but
// it must belong to the CRS version of its directory.
func TestTailoredSetups(t *testing.T) {
	for _, version := range []string{
		strings.TrimPrefix(crslatest.Version, "v"),
		strings.TrimPrefix(crslts.Version, "v"),
	} {
		t.Run(version, func(t *testing.T) {
			tailored, err := fs.ReadFile(setup(version), "@crs-setup.conf")
			require.NoError(t, err)
			require.Contains(t, string(tailored), "# OWASP CRS ver."+version+"\n")
			active := activeDirectives(string(tailored))
			vers := regexp.MustCompile(`ver:'OWASP_CRS/([^']+)'`).FindAllStringSubmatch(active, -1)
			require.NotEmpty(t, vers)
			for _, m := range vers {
				require.Equal(t, version, m[1], "@crs-setup.conf has an action for another CRS release")
			}
			setupVersions := regexp.MustCompile(`setvar:tx\.crs_setup_version=(\d+)`).FindAllStringSubmatch(active, -1)
			require.Len(t, setupVersions, 1, "@crs-setup.conf must set tx.crs_setup_version once")
			require.Equal(t, strings.ReplaceAll(version, ".", ""), setupVersions[0][1])
		})
	}
}
