// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestCheckOrBuildRustDynamicModule_Unsupported(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	tempDir := t.TempDir()

	manifest := &Manifest{
		Name:    "test-extension",
		Version: "1.0.0",
		Type:    TypeRust,
	}

	// Test with directory that has no Cargo.toml (unsupported type)
	err := CheckOrBuildDynamicModule(logger, fakeDirs, manifest, tempDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported dynamic module type")
	require.Contains(t, err.Error(), "no Cargo.toml found")
}

func TestCheckOrBuildRustDynamicModule(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	extensionPath := "../../../extensions/ip-restriction"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)
	require.Equal(t, TypeRust, manifest.Type)

	err = CheckOrBuildDynamicModule(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// Ensure the library is created with the correct name (original manifest name)
	libPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(libPath)
	require.NoError(t, err, "library should exist at %s", libPath)

	// Verify it uses the original manifest name: ip-restriction -> libip-restriction.so
	require.Contains(t, libPath, "libip-restriction.so",
		"library should be named libip-restriction.so (original manifest name)")

	// Run again to verify it uses the cached library and doesn't fail
	err = CheckOrBuildDynamicModule(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err, "should not fail when library is already cached")
}

func TestCopyExtensionManifests(t *testing.T) {
	t.Run("main manifest only", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		// Create only the root manifest.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "manifest.yaml"), []byte("name: test"), 0o600))

		require.NoError(t, copyExtensionManifests(srcDir, dstDir))

		// Main manifest should be copied.
		// #nosec G304
		data, err := os.ReadFile(filepath.Join(dstDir, "manifest.yaml"))
		require.NoError(t, err)
		require.Equal(t, "name: test", string(data))

		// manifests/ directory should not exist since there are no sub-extensions.
		_, err = os.Stat(filepath.Join(dstDir, "manifests"))
		require.True(t, os.IsNotExist(err))
	})

	t.Run("main and sub-extension manifests", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		// Create root manifest.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "manifest.yaml"), []byte("name: bundle"), 0o600))

		// Create sub-extension manifests.
		for _, sub := range []string{"cedar", "opa"} {
			subDir := filepath.Join(srcDir, sub)
			require.NoError(t, os.MkdirAll(subDir, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(subDir, "manifest.yaml"), []byte("name: "+sub), 0o600))
		}

		require.NoError(t, copyExtensionManifests(srcDir, dstDir))

		// Main manifest.
		// #nosec G304
		data, err := os.ReadFile(filepath.Join(dstDir, "manifest.yaml"))
		require.NoError(t, err)
		require.Equal(t, "name: bundle", string(data))

		// Sub-extension manifests under manifests/.
		for _, sub := range []string{"cedar", "opa"} {
			// #nosec G304
			data, err := os.ReadFile(filepath.Join(dstDir, "manifests", sub, "manifest.yaml"))
			require.NoError(t, err)
			require.Equal(t, "name: "+sub, string(data))
		}
	})

	t.Run("nested sub-extension manifests", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "manifest.yaml"), []byte("name: root"), 0o600))

		// Create a deeply nested sub-extension.
		nestedDir := filepath.Join(srcDir, "a", "b")
		require.NoError(t, os.MkdirAll(nestedDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "manifest.yaml"), []byte("name: nested"), 0o600))

		require.NoError(t, copyExtensionManifests(srcDir, dstDir))

		// #nosec G304
		data, err := os.ReadFile(filepath.Join(dstDir, "manifests", "a", "b", "manifest.yaml"))
		require.NoError(t, err)
		require.Equal(t, "name: nested", string(data))
	})

	t.Run("skips non-manifest files", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "manifest.yaml"), []byte("name: test"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "other.yaml"), []byte("other"), 0o600))

		subDir := filepath.Join(srcDir, "sub")
		require.NoError(t, os.MkdirAll(subDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "config.json"), []byte("{}"), 0o600))

		require.NoError(t, copyExtensionManifests(srcDir, dstDir))

		// Only manifest.yaml should be copied, not other files.
		_, err := os.Stat(filepath.Join(dstDir, "other.yaml"))
		require.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(dstDir, "manifests", "sub", "config.json"))
		require.True(t, os.IsNotExist(err))
	})
}

func TestCopyFile_SourceNotExists(t *testing.T) {
	tempDir := t.TempDir()

	srcFile := filepath.Join(tempDir, "nonexistent.txt")
	dstFile := filepath.Join(tempDir, "destination.txt")

	// Should fail when source doesn't exist
	err := copyFile(srcFile, dstFile)
	require.Error(t, err)
}

func TestCopyFile_InvalidDestination(t *testing.T) {
	tempDir := t.TempDir()

	srcFile := filepath.Join(tempDir, "source.txt")
	err := os.WriteFile(srcFile, []byte("test"), 0o600)
	require.NoError(t, err)

	// Try to copy to an invalid destination (e.g., directory that doesn't exist)
	dstFile := filepath.Join(tempDir, "nonexistent_dir", "destination.txt")

	err = copyFile(srcFile, dstFile)
	require.Error(t, err)
}
