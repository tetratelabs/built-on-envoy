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
// - go: extensions/goplugin/<name>/<version>
// - rust: extensions/dym/<name>/<version>
// - ext_proc: extensions/extproc/<name>/<version>
// - others: extensions/<name>/<version>
func LocalCacheExtensionDir(dirs *xdg.Directories, manifest *Manifest) string {
	switch manifest.Type {
	case TypeGo:
		return filepath.Join(dirs.DataHome, "extensions", "goplugin", manifest.Name, manifest.Version)
	case TypeRust:
		return filepath.Join(dirs.DataHome, "extensions", "dym", manifest.Name, manifest.Version)
	case TypeExtProc:
		return filepath.Join(dirs.DataHome, "extensions", "extproc", manifest.Name, manifest.Version)
	default:
		return filepath.Join(dirs.DataHome, "extensions", manifest.Name, manifest.Version)
	}
}

// LocalCacheExtension returns the local cache path for the extension library based on the manifest.
// Returns the appropriate library file name for each extension type:
// - go: plugin.so (Go plugin)
// - rust: lib<name>.so (uses original name from manifest)
// - others: plugin.so (default)
func LocalCacheExtension(dirs *xdg.Directories, manifest *Manifest) string {
	dir := LocalCacheExtensionDir(dirs, manifest)

	switch manifest.Type {
	case TypeRust:
		// Use the original manifest name for the library
		return filepath.Join(dir, fmt.Sprintf("lib%s.so", manifest.Name))
	case TypeExtProc:
		// ext_proc extensions are not Go plugins, so we return the path to the ext_proc server binary instead
		return filepath.Join(dir, "ext_proc-server")
	default:
		// Default for Go and other types
		return filepath.Join(dir, "plugin.so")
	}
}

// LocalCacheComposerSourceArtifactDir returns the local cache directory for the composer
// source artifact based on the manifest.
func LocalCacheComposerSourceArtifactDir(dirs *xdg.Directories, manifest *Manifest) string {
	return filepath.Join(dirs.DataHome, "extensions", "src", manifest.Name, manifest.Version)
}

// LocalCacheComposerExtensionSourceDir returns the local cache directory for the composer extension source code
// based on the extension name. It looks for the manifest.yaml file in the composer source artifact directory
// to find the matching extension name and returns its path.
func LocalCacheComposerExtensionSourceDir(dirs *xdg.Directories, manifest *Manifest, name string) string {
	base := LocalCacheComposerSourceArtifactDir(dirs, manifest)
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

// LocalCacheComposerDir returns the local cache directory for the composer.
func LocalCacheComposerDir(dirs *xdg.Directories, version string) string {
	return filepath.Join(dirs.DataHome, "extensions", "dym", "composer", version)
}

// LocalCacheComposerLib returns the local cache path for the composer lib.
func LocalCacheComposerLib(dirs *xdg.Directories, version string) string {
	return filepath.Join(LocalCacheComposerDir(dirs, version), "libcomposer.so")
}
