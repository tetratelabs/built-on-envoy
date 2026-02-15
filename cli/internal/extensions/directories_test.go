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

func TestLocalCacheManifest(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}

	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeDynamicModule}))

	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeComposer}))
}

func TestLocalCacheExtensionDirs(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}

	// Test dynamic_module type
	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeDynamicModule}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1/libtest.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeDynamicModule}))

	// Test dynamic_module with dashes in name (uses original name)
	require.Equal(t, "/home/user/.local/share/extensions/dym/ip-restriction/1.0.0",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "ip-restriction", Version: "1.0.0", Type: TypeDynamicModule}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/ip-restriction/1.0.0/libip-restriction.so",
		LocalCacheExtension(dirs, &Manifest{Name: "ip-restriction", Version: "1.0.0", Type: TypeDynamicModule}))

	// Test composer type
	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeComposer}))

	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1/plugin.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeComposer}))

	// Test other types (default)
	require.Equal(t, "/home/user/.local/share/extensions/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeLua}))

	require.Equal(t, "/home/user/.local/share/extensions/test/1.0.1/plugin.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeLua}))
}

func TestLocalCacheComposerDirs(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}

	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/2.0.0",
		LocalCacheComposerDir(dirs, "2.0.0"))
	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/2.0.0/libcomposer.so",
		LocalCacheComposerLib(dirs, "2.0.0"))
}

// validManifestYAML returns a valid manifest YAML string for the given extension name and type.
func validManifestYAML(name string) string {
	return `name: ` + name + `
version: 1.0.0
composerVersion: 1.0.0
categories:
  - Network
author: Test
description: Test extension
longDescription: |
  Test long description
type: composer
tags:
  - test
license: Apache-2.0
examples:
  - title: Test
    description: Test example
    code: |
      boe run --extension ` + name
}

func TestLocalCacheComposerExtensionSourceDir(t *testing.T) {
	t.Run("finds matching extension in source directory", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "1.0.0", Type: TypeComposer}

		// Create the source artifact directory structure with two extensions
		base := LocalCacheComposerSourceArtifactDir(dirs, manifest)

		ext1Dir := filepath.Join(base, "ext1")
		ext2Dir := filepath.Join(base, "ext2")
		require.NoError(t, os.MkdirAll(ext1Dir, 0o750))
		require.NoError(t, os.MkdirAll(ext2Dir, 0o750))

		require.NoError(t, os.WriteFile(filepath.Join(ext1Dir, "manifest.yaml"),
			[]byte(validManifestYAML("alpha")), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(ext2Dir, "manifest.yaml"),
			[]byte(validManifestYAML("beta")), 0o600))

		// Should find the correct extension directory
		require.Equal(t, ext1Dir, LocalCacheComposerExtensionSourceDir(dirs, manifest, "alpha"))
		require.Equal(t, ext2Dir, LocalCacheComposerExtensionSourceDir(dirs, manifest, "beta"))
	})

	t.Run("returns empty string when extension not found", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "1.0.0", Type: TypeComposer}

		// Create one extension
		base := LocalCacheComposerSourceArtifactDir(dirs, manifest)
		extDir := filepath.Join(base, "ext1")
		require.NoError(t, os.MkdirAll(extDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(extDir, "manifest.yaml"),
			[]byte(validManifestYAML("alpha")), 0o600))

		// Search for a non-existent extension
		require.Empty(t, LocalCacheComposerExtensionSourceDir(dirs, manifest, "nonexistent"))
	})

	t.Run("returns empty string when base directory does not exist", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "missing", Version: "1.0.0", Type: TypeComposer}

		// Don't create any directories base path doesn't exist
		require.Empty(t, LocalCacheComposerExtensionSourceDir(dirs, manifest, "anything"))
	})

	t.Run("returns empty string when manifest is invalid", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "1.0.0", Type: TypeComposer}

		base := LocalCacheComposerSourceArtifactDir(dirs, manifest)

		// Create a directory with an invalid manifest
		badDir := filepath.Join(base, "bad")
		require.NoError(t, os.MkdirAll(badDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(badDir, "manifest.yaml"),
			[]byte("not: valid: yaml: ["), 0o600))

		// Walk stops on error, so no result is returned
		require.Empty(t, LocalCacheComposerExtensionSourceDir(dirs, manifest, "anything"))
	})

	t.Run("handles nested directory structures", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "2.0.0", Type: TypeComposer}

		base := LocalCacheComposerSourceArtifactDir(dirs, manifest)

		// Create a nested directory with a manifest
		nestedDir := filepath.Join(base, "category", "nested-ext")
		require.NoError(t, os.MkdirAll(nestedDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "manifest.yaml"),
			[]byte(validManifestYAML("nested")), 0o600))

		require.Equal(t, nestedDir, LocalCacheComposerExtensionSourceDir(dirs, manifest, "nested"))
	})
}
