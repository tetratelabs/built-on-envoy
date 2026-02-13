// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// CheckOrBuildDynamicModule checks if a dynamic module library exists in the cache.
// If not, it builds the dynamic module from the given path.
// Currently supports Rust dynamic modules (identified by Cargo.toml).
func CheckOrBuildDynamicModule(dirs *xdg.Directories, manifest *Manifest, path string) error {
	// Check if this is a Rust dynamic module (currently the only supported type)
	cargoTomlPath := filepath.Join(path, "Cargo.toml")
	if _, err := os.Stat(cargoTomlPath); os.IsNotExist(err) {
		return fmt.Errorf("unsupported dynamic module type: no Cargo.toml found in %s", path)
	}

	// Check if the library already exists in cache.
	destLib := LocalCacheExtension(dirs, manifest)
	if _, err := os.Stat(destLib); err == nil {
		// Library already exists in cache
		return nil
	}

	// Build the Rust project and make sure the output is in current path.
	// #nosec G204
	cmd := exec.Command("cargo", "build", "--release", "--target-dir", "./target")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build Rust dynamic module from %s: %w\nOutput: %s",
			path, err, string(output))
	}

	// Rust/Cargo converts dashes to underscores in library file names internally.
	// For example, a crate named "ip-restriction" produces "libip_restriction.so".
	// We need to find this file and copy it with the original manifest name.
	rustLibName := strings.ReplaceAll(manifest.Name, "-", "_")

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

	// Create the cache directory if it doesn't exist
	cacheDir := LocalCacheExtensionDir(dirs, manifest)
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	// Copy the library to the cache (always as .so)
	if err := copyFile(srcLib, destLib); err != nil {
		return fmt.Errorf("failed to copy library from %s to %s: %w", srcLib, destLib, err)
	}

	return nil
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
