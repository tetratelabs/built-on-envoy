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
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeRust}))

	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeGo}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "composer", Version: "1.0.1", Type: TypeComposer}))

	require.Equal(t, "/home/user/.local/share/extensions/extproc/test/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeExtProc}))
}

func TestLocalCacheExtensionDirs(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}

	// Test rust type
	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeRust}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1/libtest.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeRust}))

	// Test rust with dashes in name (uses original name)
	require.Equal(t, "/home/user/.local/share/extensions/dym/ip-restriction/1.0.0",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "ip-restriction", Version: "1.0.0", Type: TypeRust}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/ip-restriction/1.0.0/libip-restriction.so",
		LocalCacheExtension(dirs, &Manifest{Name: "ip-restriction", Version: "1.0.0", Type: TypeRust}))

	// Test go type
	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeGo}))

	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1/plugin.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeGo}))

	// Test go c-shared type
	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeGo, CShared: true}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1/libtest.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeGo, CShared: true}))

	// Test ext_proc type
	require.Equal(t, "/home/user/.local/share/extensions/extproc/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeExtProc}))

	require.Equal(t, "/home/user/.local/share/extensions/extproc/test/1.0.1/ext_proc-server",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeExtProc}))

	// Test wasm type
	require.Equal(t, "/home/user/.local/share/extensions/wasm/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeWasm}))

	require.Equal(t, "/home/user/.local/share/extensions/wasm/test/1.0.1/plugin.wasm",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeWasm}))

	// Test composer type
	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "composer", Version: "1.0.1", Type: TypeComposer}))

	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/1.0.1/libcomposer.so",
		LocalCacheExtension(dirs, &Manifest{Name: "composer", Version: "1.0.1", Type: TypeComposer}))

	// Test other types (default)
	require.Equal(t, "/home/user/.local/share/extensions/test/1.0.1",
		LocalCacheExtensionDir(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeLua}))

	require.Equal(t, "/home/user/.local/share/extensions/test/1.0.1/plugin.so",
		LocalCacheExtension(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: TypeLua}))

	// Bundle-hosted extension (e.g. goplugin-loader): resolves to the shared bundle
	// library by bundle name, keyed by the manifest version, regardless of the
	// extension's own name/type.
	goPluginLoader := &Manifest{
		Name: GoPluginLoaderName, Type: TypeGo, CShared: true,
		Parent: ComposerBundle, Version: "1.0.1",
	}
	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/1.0.1",
		LocalCacheExtensionDir(dirs, goPluginLoader))
	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/1.0.1/libcomposer.so",
		LocalCacheExtension(dirs, goPluginLoader))

	// A non-composer bundle resolves to lib<bundle>.so under the bundle's dir.
	rustBundleMember := &Manifest{
		Name: "rate-limit", Type: TypeRust,
		Parent: "rustextensions", Version: "3.1.0",
	}
	require.Equal(t, "/home/user/.local/share/extensions/dym/rustextensions/3.1.0",
		LocalCacheExtensionDir(dirs, rustBundleMember))
	require.Equal(t, "/home/user/.local/share/extensions/dym/rustextensions/3.1.0/librustextensions.so",
		LocalCacheExtension(dirs, rustBundleMember))
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
type: go
tags:
  - test
license: Apache-2.0
examples:
  - title: Test
    description: Test example
    code: |
      boe run --extension ` + name
}

func TestLocalCacheExtensionSourceDir(t *testing.T) {
	t.Run("finds matching extension in source directory", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "1.0.0", Type: TypeGo}

		// Create the source artifact directory structure with two extensions
		base := LocalCacheExtensionSourceArtifactDir(dirs, manifest)

		ext1Dir := filepath.Join(base, "ext1")
		ext2Dir := filepath.Join(base, "ext2")
		require.NoError(t, os.MkdirAll(ext1Dir, 0o750))
		require.NoError(t, os.MkdirAll(ext2Dir, 0o750))

		require.NoError(t, os.WriteFile(filepath.Join(ext1Dir, "manifest.yaml"),
			[]byte(validManifestYAML("alpha")), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(ext2Dir, "manifest.yaml"),
			[]byte(validManifestYAML("beta")), 0o600))

		// Should find the correct extension directory
		require.Equal(t, ext1Dir, LocalCacheExtensionSourceDir(dirs, manifest, "alpha"))
		require.Equal(t, ext2Dir, LocalCacheExtensionSourceDir(dirs, manifest, "beta"))
	})

	t.Run("returns empty string when extension not found", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "1.0.0", Type: TypeGo}

		// Create one extension
		base := LocalCacheExtensionSourceArtifactDir(dirs, manifest)
		extDir := filepath.Join(base, "ext1")
		require.NoError(t, os.MkdirAll(extDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(extDir, "manifest.yaml"),
			[]byte(validManifestYAML("alpha")), 0o600))

		// Search for a non-existent extension
		require.Empty(t, LocalCacheExtensionSourceDir(dirs, manifest, "nonexistent"))
	})

	t.Run("returns empty string when base directory does not exist", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "missing", Version: "1.0.0", Type: TypeGo}

		// Don't create any directories base path doesn't exist
		require.Empty(t, LocalCacheExtensionSourceDir(dirs, manifest, "anything"))
	})

	t.Run("returns empty string when manifest is invalid", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "1.0.0", Type: TypeGo}

		base := LocalCacheExtensionSourceArtifactDir(dirs, manifest)

		// Create a directory with an invalid manifest
		badDir := filepath.Join(base, "bad")
		require.NoError(t, os.MkdirAll(badDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(badDir, "manifest.yaml"),
			[]byte("not: valid: yaml: ["), 0o600))

		// Walk stops on error, so no result is returned
		require.Empty(t, LocalCacheExtensionSourceDir(dirs, manifest, "anything"))
	})

	t.Run("handles nested directory structures", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-set", Version: "2.0.0", Type: TypeGo}

		base := LocalCacheExtensionSourceArtifactDir(dirs, manifest)

		// Create a nested directory with a manifest
		nestedDir := filepath.Join(base, "category", "nested-ext")
		require.NoError(t, os.MkdirAll(nestedDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "manifest.yaml"),
			[]byte(validManifestYAML("nested")), 0o600))

		require.Equal(t, nestedDir, LocalCacheExtensionSourceDir(dirs, manifest, "nested"))
	})
}

func TestLocalCacheExtensionManifest(t *testing.T) {
	t.Run("standalone source artifact", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-ext", Version: "1.0.0", Type: TypeGo}

		path, err := LocalCacheExtensionManifest(dirs, manifest, ArtifactSource, "my-ext")
		require.NoError(t, err)
		require.Equal(t,
			filepath.Join(LocalCacheExtensionSourceArtifactDir(dirs, manifest), "manifest.yaml"),
			path)
	})

	t.Run("standalone binary artifact", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-ext", Version: "1.0.0", Type: TypeRust}

		path, err := LocalCacheExtensionManifest(dirs, manifest, ArtifactBinary, "my-ext")
		require.NoError(t, err)
		require.Equal(t,
			filepath.Join(LocalCacheExtensionDir(dirs, manifest), "manifest.yaml"),
			path)
	})

	t.Run("standalone with empty extension name", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-ext", Version: "1.0.0", Type: TypeGo}

		path, err := LocalCacheExtensionManifest(dirs, manifest, ArtifactSource, "")
		require.NoError(t, err)
		require.Equal(t,
			filepath.Join(LocalCacheExtensionSourceArtifactDir(dirs, manifest), "manifest.yaml"),
			path)
	})

	t.Run("bundle finds child extension manifest", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-bundle", Version: "2.0.0", Type: TypeGo}

		// Create the source artifact directory with a child extension
		base := LocalCacheExtensionSourceArtifactDir(dirs, manifest)
		childDir := filepath.Join(base, "child-ext")
		require.NoError(t, os.MkdirAll(childDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "manifest.yaml"),
			[]byte(validManifestYAML("child-ext")), 0o600))

		path, err := LocalCacheExtensionManifest(dirs, manifest, ArtifactSource, "child-ext")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(childDir, "manifest.yaml"), path)
	})

	t.Run("bundle child not found returns error", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		manifest := &Manifest{Name: "my-bundle", Version: "2.0.0", Type: TypeGo}

		// Create the base directory but no child
		base := LocalCacheExtensionSourceArtifactDir(dirs, manifest)
		require.NoError(t, os.MkdirAll(base, 0o750))

		_, err := LocalCacheExtensionManifest(dirs, manifest, ArtifactSource, "missing-child")
		require.Error(t, err)
		require.Contains(t, err.Error(), `extension "missing-child" not found`)
	})
}
