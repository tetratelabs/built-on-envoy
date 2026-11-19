// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package crsregistry

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// fakeCRS returns the files of a fake CRS version, with the crs/v4 or the lts/v4 module names.
func fakeCRS(version string, ltsNames bool) Version {
	rulesDir := "@owasp_crs"
	if ltsNames {
		rulesDir = "@owasp_crs_lts"
	}
	return Version{
		Version: version,
		Rules: fstest.MapFS{
			rulesDir + "/REQUEST-901-INITIALIZATION.conf": {Data: []byte(`SecAction "id:901001,ver:'OWASP_CRS/` + version + `'"`)},
		},
		Setup: fstest.MapFS{"@crs-setup.conf": {Data: []byte("# OWASP CRS ver." + version + "\n")}},
	}
}

func TestRegister(t *testing.T) {
	r := &Registry{}
	require.NoError(t, r.Register(fakeCRS("4.30.0", false)))
	require.NoError(t, r.Register(fakeCRS("4.25.1", true)))
	require.NoError(t, r.SetDefault("4.25.1"))

	require.NoError(t, r.SetAlias("latest", "4.30.0"))
	require.NoError(t, r.SetAlias("lts", "4.25.1"))
	require.NoError(t, r.SetAlias("4.25.1", "4.25.1"))

	snap, err := r.Freeze()
	require.NoError(t, err)
	require.Equal(t, "4.25.1", snap.Default)
	require.Equal(t, map[string]string{"latest": "4.30.0", "lts": "4.25.1", "4.25.1": "4.25.1"}, snap.Aliases)
	require.Len(t, snap.Entries, 2)
	require.Equal(t, "4.30.0", snap.Entries[0].Version.Version)
	require.False(t, snap.Entries[0].LTSNames)
	require.Equal(t, "4.25.1", snap.Entries[1].Version.Version)
	require.True(t, snap.Entries[1].LTSNames)
}

func TestSetAliasLastWins(t *testing.T) {
	r := &Registry{}
	require.NoError(t, r.Register(fakeCRS("4.25.2", true)))
	require.NoError(t, r.Register(fakeCRS("4.25.0", true)))
	require.NoError(t, r.SetDefault("4.25.0"))
	require.NoError(t, r.SetAlias("lts", "4.25.0"))
	require.NoError(t, r.SetAlias("lts", "4.25.2"))

	snap, err := r.Freeze()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"lts": "4.25.2"}, snap.Aliases)
	require.Equal(t, "4.25.0", snap.Default)
}

func TestSetDefaultLastWins(t *testing.T) {
	r := &Registry{}
	require.NoError(t, r.Register(fakeCRS("4.25.2", true)))
	require.NoError(t, r.Register(fakeCRS("4.25.0", true)))
	require.NoError(t, r.SetDefault("4.25.2"))
	require.NoError(t, r.SetDefault("4.25.0"))

	snap, err := r.Freeze()
	require.NoError(t, err)
	require.Equal(t, "4.25.0", snap.Default)
}

func TestRegisterErrors(t *testing.T) {
	mismatchedSetup := fakeCRS("4.25.1", true)
	mismatchedSetup.Setup = fakeCRS("4.25.0", true).Setup
	mismatchedRules := fakeCRS("4.25.1", true)
	mismatchedRules.Rules = fakeCRS("4.25.0", true).Rules

	for name, tc := range map[string]struct {
		v       Version
		wantErr string
	}{
		"not an exact version":   {v: fakeCRS("4.25", false), wantErr: "invalid version"},
		"v prefix":               {v: fakeCRS("v4.25.0", false), wantErr: "invalid version"},
		"no rules":               {v: Version{Version: "4.25.0", Setup: fakeCRS("4.25.0", false).Setup}, wantErr: "required"},
		"no tailored setup":      {v: Version{Version: "4.25.0", Rules: fakeCRS("4.25.0", false).Rules, Setup: fstest.MapFS{}}, wantErr: "tailored setup"},
		"setup of another CRS":   {v: mismatchedSetup, wantErr: "not the setup of CRS 4.25.1"},
		"rules of another CRS":   {v: mismatchedRules, wantErr: "not those of CRS 4.25.1"},
		"rules without CRS init": {v: Version{Version: "4.25.0", Rules: fstest.MapFS{}, Setup: fakeCRS("4.25.0", false).Setup}, wantErr: "reading the rules"},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, (&Registry{}).Register(tc.v), tc.wantErr)
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Register(fakeCRS("4.25.0", false)))
		require.ErrorContains(t, r.Register(fakeCRS("4.25.0", true)), "already registered")
	})
	t.Run("default not registered", func(t *testing.T) {
		require.ErrorContains(t, (&Registry{}).SetDefault("4.25.0"), "unregistered")
	})
	t.Run("no default", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Register(fakeCRS("4.25.0", false)))
		_, err := r.Freeze()
		require.ErrorContains(t, err, "no default")
	})
	t.Run("version alias of another version", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Register(fakeCRS("4.25.0", false)))
		require.ErrorContains(t, r.SetAlias("4.25.2", "4.25.0"), "cannot select version 4.25.0")
	})
	t.Run("alias not registered", func(t *testing.T) {
		require.ErrorContains(t, (&Registry{}).SetAlias("lts", "4.25.0"), "unregistered")
	})
	for _, alias := range []string{"", "LTS", "4.25", "v4.25.0", "lts-4"} {
		t.Run("invalid alias "+alias, func(t *testing.T) {
			r := &Registry{}
			require.NoError(t, r.Register(fakeCRS("4.25.0", false)))
			require.ErrorContains(t, r.SetAlias(alias, "4.25.0"), "invalid alias")
		})
	}
}

func TestFrozen(t *testing.T) {
	r := &Registry{}
	require.NoError(t, r.Register(fakeCRS("4.25.0", false)))
	require.NoError(t, r.Register(fakeCRS("4.25.2", false)))
	require.NoError(t, r.SetDefault("4.25.0"))
	require.NoError(t, r.SetAlias("lts", "4.25.0"))
	_, err := r.Freeze()
	require.NoError(t, err)

	require.ErrorContains(t, r.Register(fakeCRS("4.25.1", false)), "after the WAF configuration was loaded")
	require.ErrorContains(t, r.SetDefault("4.25.2"), "after the WAF configuration was loaded")
	require.ErrorContains(t, r.SetAlias("lts", "4.25.2"), "after the WAF configuration was loaded")

	snap, err := r.Freeze()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 2)
	require.Equal(t, "4.25.0", snap.Default)
	require.Equal(t, map[string]string{"lts": "4.25.0"}, snap.Aliases)
}
