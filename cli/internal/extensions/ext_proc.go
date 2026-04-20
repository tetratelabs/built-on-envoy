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

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// CheckOrBuildExtProcBinary checks if an ext_proc server binary exists in the cache.
// If not, it builds the binary from the given path.
// The extension directory must contain a go.mod file.
func CheckOrBuildExtProcBinary(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	goModPath := filepath.Join(path, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return fmt.Errorf("unsupported ext_proc extension: no go.mod found in %s", path)
	}

	destBin := LocalCacheExtension(dirs, manifest)
	if _, err := os.Stat(destBin); err == nil {
		logger.Debug("ext_proc binary already exists in cache, skipping build", "path", destBin)
		return nil
	}

	return BuildExtProcBinary(logger, dirs, manifest, path)
}

// BuildExtProcBinary builds the ext_proc server binary from the given path and saves it to the cache.
func BuildExtProcBinary(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	destBin := LocalCacheExtension(dirs, manifest)

	cacheDir := LocalCacheExtensionDir(dirs, manifest)
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	// #nosec G204
	cmd := exec.Command("go", "build", "-a", "-ldflags=-s -w", "-o", destBin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Debug("building ext_proc server binary", "cmd", cmd.String(), "output", destBin)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build ext_proc server from %s: %w", path, err)
	}

	logger.Debug("ext_proc server binary built and cached", "path", destBin)

	return nil
}
