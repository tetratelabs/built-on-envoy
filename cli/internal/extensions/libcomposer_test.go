// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestLibComposerVersion(t *testing.T) {
	require.Truef(t, semver.IsValid("v"+LibComposerVersion),
		"ComposerVersion %q is not a valid semver", LibComposerVersion)
}

func TestDownloadLibComposerAndBuildIfNeeded_DownloadError(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	d := &Downloader{
		Logger:   logger,
		Registry: "ghcr.io/test",
		Dirs:     fakeDirs,
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	err := DownloadLibComposerAndBuildIfNeeded(t.Context(), d, "0.1.0", ComposerArtifactLite)
	require.ErrorContains(t, err, "failed to download libcomposer")
}

func TestBuildLibComposer_InvalidPath(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	err := BuildLibComposer(logger, fakeDirs, "/nonexistent/path", "0.1.0", false)
	require.ErrorContains(t, err, "failed to build libcomposer from source")
}

func TestBuildLibComposer(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	composerPath := "../../../extensions/composer"
	err := BuildLibComposer(logger, fakeDirs, composerPath, LibComposerVersion, true)
	require.NoError(t, err)

	// Ensure the libcomposer.so is created.
	_, err = os.Stat(LocalCacheComposerLib(fakeDirs, LibComposerVersion))
	require.NoError(t, err)

	// Ensure plugins are built
	_, err = os.Stat(LocalCacheExtension(fakeDirs, Manifests["example-go"]))
	require.NoError(t, err)
}

func TestBuildExtensionFromPath_CShared(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	// Create a fake extension directory with a main/ subdirectory to trigger c-shared build.
	extDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, "main"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "main", "main.go"), []byte("package main\nfunc main() {}\n"), 0o600))

	manifest := &Manifest{Name: "test-cshared", Version: "0.0.1"}
	cshared, err := BuildExtensionFromPath(logger, fakeDirs, manifest, extDir)
	// The build may fail (no exported symbols for c-shared), but we exercise the code path.
	if err != nil {
		require.True(t, cshared)
		require.ErrorContains(t, err, "failed to build local extension")
	} else {
		require.True(t, cshared)
	}
}

func TestBuildExtensionFromPath(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	extensionPath := "../../../extensions/composer/example"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)

	cshared, err := BuildExtensionFromPath(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// The example extension does not have a main/ directory, so it should be built as a plugin.
	require.False(t, cshared)

	pluginPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(pluginPath)
	require.NoError(t, err)
}
