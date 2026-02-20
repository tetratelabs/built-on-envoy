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

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestLibGoBundleVersion(t *testing.T) {
	require.Truef(t, semver.IsValid("v"+LibGoBundleVersion),
		"GoBundleVersion %q is not a valid semver", LibGoBundleVersion)
}

func TestBuildLibGoBundle(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	gobundlePath := "../../../extensions/go_bundle"
	err := BuildLibGoBundle(logger, fakeDirs, gobundlePath, LibGoBundleVersion, true)
	require.NoError(t, err)

	// Ensure the libgobundle.so is created.
	_, err = os.Stat(LocalCacheGoBundleLib(fakeDirs, LibGoBundleVersion))
	require.NoError(t, err)

	// Ensure plugins are built
	_, err = os.Stat(LocalCacheExtension(fakeDirs, Manifests["example-go"]))
	require.NoError(t, err)
}

func TestBuildExtensionFromPath(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	extensionPath := "../../../extensions/go_bundle/example"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)

	err = BuildExtensionFromPath(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// Ensure the plugin.so is created.
	pluginPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(pluginPath)
	require.NoError(t, err)
}
