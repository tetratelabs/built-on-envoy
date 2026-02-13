// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestLocalCacheManifest(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}

	require.Equal(t, "/home/user/.local/share/extensions/dym/test/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: "dynamic_module"}))

	require.Equal(t, "/home/user/.local/share/extensions/goplugin/test/1.0.1/manifest.yaml",
		LocalCacheManifest(dirs, &Manifest{Name: "test", Version: "1.0.1", Type: "composer"}))
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
		LocalCacheComposerDir(dirs, "2.0.0", false))
	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/2.0.0/libcomposer.so",
		LocalCacheComposerLib(dirs, "2.0.0", false))

	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/build/2.0.0",
		LocalCacheComposerDir(dirs, "2.0.0", true))
	require.Equal(t, "/home/user/.local/share/extensions/dym/composer/build/2.0.0/libcomposer.so",
		LocalCacheComposerLib(dirs, "2.0.0", true))
}
