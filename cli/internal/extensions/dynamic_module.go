// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// CheckOrBuildDynamicModule checks if a dynamic module library exists in the cache.
// If not, it builds the dynamic module from the given path.
// Currently supports Rust dynamic modules (identified by Cargo.toml).
func CheckOrBuildDynamicModule(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	// Check if this is a Rust dynamic module (currently the only supported type)
	cargoTomlPath := filepath.Join(path, "Cargo.toml")
	if _, err := os.Stat(cargoTomlPath); os.IsNotExist(err) {
		return fmt.Errorf("unsupported dynamic module type: no Cargo.toml found in %s", path)
	}

	// Check if the library already exists in cache.
	destLib := LocalCacheExtension(dirs, manifest)
	if _, err := os.Stat(destLib); err == nil {
		// Library already exists in cache
		logger.Debug("dynamic module library already exists in cache, skipping build", "path", destLib)
		return nil
	}

	return BuildDynamicModule(logger, dirs, manifest, path)
}

// BuildDynamicModule builds the dynamic module from source. The source code is expected to be at the given path.
// The built library will be saved in the local cache directory.
func BuildDynamicModule(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	destLib := LocalCacheExtension(dirs, manifest)

	// Build the Rust project and make sure the output is in current path.
	// #nosec G204
	cmd := exec.Command("cargo", "build", "--release", "--target-dir", "./target")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Debug("building Rust dynamic module", "cmd", cmd.String())

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to build Rust dynamic module from %s: %w",
			path, err)
	}

	// Rust/Cargo converts dashes to underscores in library file names internally.
	// For example, a crate named "ip-restriction" produces "libip_restriction.so".
	// We need to find this file and copy it with the original manifest name.
	rustLibName := RustLibNameFromName(manifest.Name)

	// Find the built library in target/release
	// Note: Cargo may build with platform-specific extension (.dylib on macOS), but we use .so
	var srcLib string
	for _, ext := range []string{"so", "dylib"} {
		candidate := filepath.Join(path, "target", "release", fmt.Sprintf("lib%s.%s", rustLibName, ext))
		if _, err := os.Stat(candidate); err == nil {
			srcLib = candidate
			break
		}
	}
	if srcLib == "" {
		return fmt.Errorf("built library not found at %s/target/release/lib%s.{so,dylib}", path, rustLibName)
	}

	logger.Debug("built Rust dynamic module library", "lib", srcLib)

	// Create the cache directory if it doesn't exist
	cacheDir := LocalCacheExtensionDir(dirs, manifest)
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	// Copy the library to the cache (always as .so)
	if err := copyFile(srcLib, destLib); err != nil {
		return fmt.Errorf("failed to copy library from %s to %s: %w", srcLib, destLib, err)
	}

	logger.Debug("dynamic module library copied to cache", "path", destLib)

	return nil
}

// RustLibNameFromName converts the extension name to a valid Rust library name by replacing hyphens with underscores.
func RustLibNameFromName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	// #nosec G304 -- src is a trusted path from the build output
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// #nosec G306 -- dynamic module library needs executable permissions
	return os.WriteFile(dst, data, 0o755)
}
