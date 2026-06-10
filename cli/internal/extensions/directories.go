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

// LocalCacheExtensionManifest returns the path to the manifest.yaml of the extension named
// extensionName within a downloaded artifact. For a standalone artifact (extensionName matches the
// artifact, or is empty) it is the manifest at the artifact root; otherwise the artifact is treated
// as a bundle and its source tree is walked to find the named child's manifest.
func LocalCacheExtensionManifest(dirs *xdg.Directories, artifactManifest *Manifest, artifactType,
	extensionName string,
) (string, error) {
	var base string
	if artifactType == ArtifactSource {
		base = LocalCacheExtensionSourceArtifactDir(dirs, artifactManifest)
	} else {
		base = LocalCacheExtensionDir(dirs, artifactManifest)
	}

	if artifactManifest.Name == extensionName || extensionName == "" {
		// Standalone extension, manifest is at the root of the extension directory.
		return filepath.Join(base, "manifest.yaml"), nil
	}
	// Find the extension manifest by walking the extension directory.
	path, err := FindExtensionPath(base, extensionName)
	if err != nil {
		return "", err
	}
	return filepath.Join(path, "manifest.yaml"), nil
}

// ModuleName returns the dynamic-module name for the manifest. Bundle-hosted
// extensions (e.g. goplugin-loader) are served by a shared bundle library named
// after the bundle (e.g. libcomposer.so); otherwise the extension's own name.
func ModuleName(manifest *Manifest) string {
	// A Go plugin (not c-shared) is loaded by composer and has no bundle library of its own.
	if manifest.Type == TypeGo && !manifest.CShared {
		return manifest.Name
	}

	if manifest.Parent != "" {
		return manifest.Parent
	}
	return manifest.Name
}

// LocalCacheExtensionDir returns the local cache directory for the given extension manifest.
// Different extension types are organized in different directory structures:
//   - go: extensions/goplugin/<name>/<version> for plugins,
//     or extensions/dym/<name>/<version> when manifest.CShared is true
//   - rust: extensions/dym/<name>/<version>
//   - ext_proc: extensions/extproc/<name>/<version>
//   - others: extensions/<name>/<version>
func LocalCacheExtensionDir(dirs *xdg.Directories, manifest *Manifest) string {
	moduleName := ModuleName(manifest)

	switch manifest.Type {
	case TypeGo:
		if manifest.CShared {
			return filepath.Join(dirs.DataHome, "extensions", "dym", moduleName, manifest.Version)
		}
		return filepath.Join(dirs.DataHome, "extensions", "goplugin", moduleName, manifest.Version)
	case TypeRust:
		return filepath.Join(dirs.DataHome, "extensions", "dym", moduleName, manifest.Version)
	case TypeExtProc:
		return filepath.Join(dirs.DataHome, "extensions", "extproc", moduleName, manifest.Version)
	case TypeComposer:
		return LocalCacheComposerDir(dirs, manifest.Version)
	default:
		return filepath.Join(dirs.DataHome, "extensions", moduleName, manifest.Version)
	}
}

// LocalCacheExtension returns the local cache path for the extension library based on the manifest.
// Returns the appropriate library file name for each extension type:
// - go: plugin.so for plugins, or lib<name>.so when manifest.CShared is true
// - rust: lib<name>.so (uses original name from manifest)
// - ext_proc: ext_proc-server (standalone binary)
// - others: plugin.so (default)
func LocalCacheExtension(dirs *xdg.Directories, manifest *Manifest) string {
	dir := LocalCacheExtensionDir(dirs, manifest)

	switch manifest.Type {
	case TypeGo:
		if manifest.CShared {
			return filepath.Join(dir, fmt.Sprintf("lib%s.so", ModuleName(manifest)))
		}
		return filepath.Join(dir, "plugin.so")
	case TypeRust:
		// Use the original manifest name for the library
		return filepath.Join(dir, fmt.Sprintf("lib%s.so", ModuleName(manifest)))
	case TypeExtProc:
		// ext_proc extensions are not Go plugins, so we return the path to the ext_proc server binary instead
		return filepath.Join(dir, "ext_proc-server")
	case TypeComposer:
		return LocalCacheComposerLib(dirs, manifest.Version)
	default:
		return filepath.Join(dir, "plugin.so")
	}
}

// LocalCacheExtensionSourceArtifactDir returns the local cache directory for a downloaded source
// artifact (extensions/src/<name>/<version>), keyed by its manifest. Used for any source artifact
// that is built on the client — the composer and general bundles alike.
func LocalCacheExtensionSourceArtifactDir(dirs *xdg.Directories, manifest *Manifest) string {
	moduleName := ModuleName(manifest)
	return filepath.Join(dirs.DataHome, "extensions", "src", moduleName, manifest.Version)
}

// LocalCacheExtensionSourceDir returns the source directory of the child extension named `name`
// within a downloaded source artifact, by walking the extracted tree for the manifest.yaml whose
// name matches. Used for composer plugins and general bundle children alike.
func LocalCacheExtensionSourceDir(dirs *xdg.Directories, manifest *Manifest, name string) string {
	path, _ := FindExtensionPath(LocalCacheExtensionSourceArtifactDir(dirs, manifest), name)
	return path
}

// LocalCacheComposerDir returns the local cache directory for the composer.
func LocalCacheComposerDir(dirs *xdg.Directories, version string) string {
	return filepath.Join(dirs.DataHome, "extensions", "dym", "composer", version)
}

// LocalCacheComposerLib returns the local cache path for the composer lib.
func LocalCacheComposerLib(dirs *xdg.Directories, version string) string {
	return filepath.Join(LocalCacheComposerDir(dirs, version), "libcomposer.so")
}

// LocalCacheComposerSourceDir returns the local cache directory of the composer source artifact
// for the given version (extensions/src/composer/<version>).
func LocalCacheComposerSourceDir(dirs *xdg.Directories, version string) string {
	return filepath.Join(dirs.DataHome, "extensions", "src", "composer", version)
}

// FindExtensionPath walks the directory tree rooted at base and returns the directory containing
// the manifest.yaml whose name matches extensionName. It returns an error if the tree cannot be
// walked or no matching extension is found.
func FindExtensionPath(base string, extensionName string) (string, error) {
	var extensionPath string

	if err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "manifest.yaml" {
			m, err := LoadLocalManifest(path)
			if err != nil {
				return err
			}
			if m.Name == extensionName {
				extensionPath = filepath.Dir(path)
				return filepath.SkipDir
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if extensionPath == "" {
		return "", fmt.Errorf("extension %q not found in %s", extensionName, base)
	}
	return extensionPath, nil
}
