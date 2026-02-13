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

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestCheckOrBuildDynamicModule_Unsupported(t *testing.T) {
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	tempDir := t.TempDir()

	manifest := &Manifest{
		Name:    "test-extension",
		Version: "1.0.0",
		Type:    TypeDynamicModule,
	}

	// Test with directory that has no Cargo.toml (unsupported type)
	err := CheckOrBuildDynamicModule(fakeDirs, manifest, tempDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported dynamic module type")
	require.Contains(t, err.Error(), "no Cargo.toml found")
}

func TestCheckOrBuildDynamicModule(t *testing.T) {
	extensionPath := "../../../extensions/ip-restriction"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)
	require.Equal(t, TypeDynamicModule, manifest.Type)

	err = CheckOrBuildDynamicModule(fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// Ensure the library is created with the correct name (original manifest name)
	libPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(libPath)
	require.NoError(t, err, "library should exist at %s", libPath)

	// Verify it uses the original manifest name: ip-restriction -> libip-restriction.so
	require.Contains(t, libPath, "libip-restriction.so",
		"library should be named libip-restriction.so (original manifest name)")

	// Run again to verify it uses the cached library and doesn't fail
	err = CheckOrBuildDynamicModule(fakeDirs, manifest, extensionPath)
	require.NoError(t, err, "should not fail when library is already cached")
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
