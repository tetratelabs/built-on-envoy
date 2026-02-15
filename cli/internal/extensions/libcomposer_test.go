// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestLibComposerVersion(t *testing.T) {
	require.Truef(t, semver.IsValid("v"+LibComposerVersion),
		"ComposerVersion %q is not a valid semver", LibComposerVersion)
}

func TestBuildLibComposer(t *testing.T) {
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	composerPath := "../../../extensions/composer"
	err := BuildLibComposer(fakeDirs.DataHome, composerPath, true)
	require.NoError(t, err)

	// Ensure the libcomposer.so is created.
	_, err = os.Stat(LocalCacheComposerLib(fakeDirs, LibComposerVersion))
	require.NoError(t, err)

	// Ensure plugins are built
	_, err = os.Stat(LocalCacheExtension(fakeDirs, Manifests["example-go"]))
	require.NoError(t, err)
}

func TestBuildExtensionFromPath(t *testing.T) {
	extensionPath := "../../../extensions/composer/example"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)

	err = BuildExtensionFromPath(fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// Ensure the plugin.so is created.
	pluginPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(pluginPath)
	require.NoError(t, err)
}
