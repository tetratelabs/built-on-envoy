// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"fmt"
	"path/filepath"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// LocalCacheManifest returns the local cache path for the manifest.yaml file of the extension based on the manifest.
func LocalCacheManifest(dirs *xdg.Directories, manifest *Manifest) string {
	return filepath.Join(LocalCacheExtensionDir(dirs, manifest), "manifest.yaml")
}

// LocalCacheExtensionDir returns the local cache directory for the given extension manifest.
// Different extension types are organized in different directory structures:
// - composer: extensions/goplugin/<name>/<version>
// - dynamic_module: extensions/dym/<name>/<version>
// - others: extensions/<name>/<version>
func LocalCacheExtensionDir(dirs *xdg.Directories, manifest *Manifest) string {
	switch manifest.Type {
	case TypeComposer:
		return filepath.Join(dirs.DataHome, "extensions", "goplugin", manifest.Name, manifest.Version)
	case TypeDynamicModule:
		return filepath.Join(dirs.DataHome, "extensions", "dym", manifest.Name, manifest.Version)
	default:
		return filepath.Join(dirs.DataHome, "extensions", manifest.Name, manifest.Version)
	}
}

// LocalCacheExtension returns the local cache path for the extension library based on the manifest.
// Returns the appropriate library file name for each extension type:
// - composer: plugin.so (Go plugin)
// - dynamic_module: lib<name>.so (uses original name from manifest)
// - others: plugin.so (default)
func LocalCacheExtension(dirs *xdg.Directories, manifest *Manifest) string {
	dir := LocalCacheExtensionDir(dirs, manifest)

	switch manifest.Type {
	case TypeDynamicModule:
		// Use the original manifest name for the library
		return filepath.Join(dir, fmt.Sprintf("lib%s.so", manifest.Name))
	default:
		// Default for composer and other types
		return filepath.Join(dir, "plugin.so")
	}
}

// LocalCacheComposerDir returns the local cache directory for the composer.
func LocalCacheComposerDir(dirs *xdg.Directories, version string, localBuild bool) string {
	// When localBuild is true, we return the path for the locally built composer lib.
	// We keep the locally build composer separate from the binary downloaded one to avoid potential
	// conflicts and allow parallel existence of both versions for testing and development purposes.
	if localBuild {
		return filepath.Join(dirs.DataHome, "extensions", "dym", "composer", "build", version)
	}
	return filepath.Join(dirs.DataHome, "extensions", "dym", "composer", version)
}

// LocalCacheComposerLib returns the local cache path for the composer lib.
func LocalCacheComposerLib(dirs *xdg.Directories, version string, localBuild bool) string {
	return filepath.Join(LocalCacheComposerDir(dirs, version, localBuild), "libcomposer.so")
}
