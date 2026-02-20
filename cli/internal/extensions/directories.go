// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// LocalCacheManifest returns the local cache path for the manifest.yaml file of the extension based on the manifest.
func LocalCacheManifest(dirs *xdg.Directories, manifest *Manifest) string {
	return filepath.Join(LocalCacheExtensionDir(dirs, manifest), "manifest.yaml")
}

// LocalCacheExtensionDir returns the local cache directory for the given extension manifest.
// Different extension types are organized in different directory structures:
// - go_bundle: extensions/goplugin/<name>/<version>
// - dynamic_module: extensions/dym/<name>/<version>
// - others: extensions/<name>/<version>
func LocalCacheExtensionDir(dirs *xdg.Directories, manifest *Manifest) string {
	switch manifest.Type {
	case TypeGoBundle:
		return filepath.Join(dirs.DataHome, "extensions", "goplugin", manifest.Name, manifest.Version)
	case TypeDynamicModule:
		return filepath.Join(dirs.DataHome, "extensions", "dym", manifest.Name, manifest.Version)
	default:
		return filepath.Join(dirs.DataHome, "extensions", manifest.Name, manifest.Version)
	}
}

// LocalCacheExtension returns the local cache path for the extension library based on the manifest.
// Returns the appropriate library file name for each extension type:
// - go_bundle: plugin.so (Go plugin)
// - dynamic_module: lib<name>.so (uses original name from manifest)
// - others: plugin.so (default)
func LocalCacheExtension(dirs *xdg.Directories, manifest *Manifest) string {
	dir := LocalCacheExtensionDir(dirs, manifest)

	switch manifest.Type {
	case TypeDynamicModule:
		// Use the original manifest name for the library
		return filepath.Join(dir, fmt.Sprintf("lib%s.so", manifest.Name))
	default:
		// Default for go_bundle and other types
		return filepath.Join(dir, "plugin.so")
	}
}

// LocalCacheGoBundleSourceArtifactDir returns the local cache directory for the go_bundle
// source artifact based on the manifest.
func LocalCacheGoBundleSourceArtifactDir(dirs *xdg.Directories, manifest *Manifest) string {
	return filepath.Join(dirs.DataHome, "extensions", "src", manifest.Name, manifest.Version)
}

// LocalCacheGoBundleExtensionSourceDir returns the local cache directory for the go_bundle extension source code
// based on the extension name. It looks for the manifest.yaml file in the go_bundle source artifact directory
// to find the matching extension name and returns its path.
func LocalCacheGoBundleExtensionSourceDir(dirs *xdg.Directories, manifest *Manifest, name string) string {
	base := LocalCacheGoBundleSourceArtifactDir(dirs, manifest)
	var extensionPath string

	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "manifest.yaml" {
			m, err := LoadLocalManifest(path)
			if err != nil {
				return err
			}

			if m.Name == name {
				extensionPath = filepath.Dir(path)
				return filepath.SkipDir
			}
		}
		return nil
	})

	return extensionPath
}

// LocalCacheGoBundleDir returns the local cache directory for the go_bundle.
func LocalCacheGoBundleDir(dirs *xdg.Directories, version string) string {
	return filepath.Join(dirs.DataHome, "extensions", "dym", "go_bundle", version)
}

// LocalCacheGoBundleLib returns the local cache path for the go_bundle lib.
func LocalCacheGoBundleLib(dirs *xdg.Directories, version string) string {
	return filepath.Join(LocalCacheGoBundleDir(dirs, version), "libgobundle.so")
}
