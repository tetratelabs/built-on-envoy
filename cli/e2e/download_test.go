// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestDownloadExtension(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	for _, ext := range []*extensions.Manifest{
		{Name: "ip-restriction", Version: "0.2.0", Type: extensions.TypeRust},
		{Name: "example-go", Version: "0.6.0", Type: extensions.TypeGo},
	} {
		t.Run(ext.Name, func(t *testing.T) {
			path := t.TempDir()
			dirs := &xdg.Directories{DataHome: path}

			requireDownloadHasFiles(t, ext,
				filepath.Base(extensions.LocalCacheExtension(dirs, ext)),
			)
		})
	}
}

func TestDownloadComposer(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	for _, variant := range []string{extensions.ComposerArtifact, extensions.ComposerArtifactLite} {
		t.Run(variant, func(t *testing.T) {
			path := t.TempDir()
			dirs := &xdg.Directories{DataHome: path}
			manifest := &extensions.Manifest{Name: variant, Version: "0.6.0", Type: extensions.TypeComposer}

			requireDownloadHasFiles(t, manifest,
				filepath.Base(extensions.LocalCacheComposerLib(dirs, "0.6.0")),
			)
		})
	}
}

func TestDownloadComposerSource(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	path := t.TempDir()

	process := internaltesting.RunCLI(t, cliBin,
		"download",
		extensions.ComposerArtifactSource+":0.3.0",
		"--path", path)

	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	dirs := &xdg.Directories{DataHome: path}
	manifest := &extensions.Manifest{Name: "composer", Version: "0.3.0"}
	require.DirExists(t, extensions.LocalCacheExtensionSourceArtifactDir(dirs, manifest))
	require.DirExists(t, extensions.LocalCacheExtensionSourceDir(dirs, manifest, "example-go"))
}

func requireDownloadHasFiles(t *testing.T, manifest *extensions.Manifest, files ...string) {
	downloadPath := t.TempDir()
	process := internaltesting.RunCLI(t, cliBin,
		"download",
		fmt.Sprintf("%s:%s", manifest.Name, manifest.Version),
		"--platform", fmt.Sprintf("linux/%s", runtime.GOARCH),
		"--path", downloadPath)

	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	dirs := &xdg.Directories{DataHome: downloadPath}
	downloadedExtension := extensions.LocalCacheExtensionDir(dirs, manifest)

	for _, file := range files {
		require.FileExists(t, filepath.Join(downloadedExtension, file))
	}
}
