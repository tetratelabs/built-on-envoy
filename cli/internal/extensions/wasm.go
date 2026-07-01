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

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// BuildWasm compiles the Go Wasm module from the given path and saves it to the cache.
//
// The module is built with the standard Go toolchain targeting WASI
// (GOOS=wasip1 GOARCH=wasm) in c-shared mode, which produces a reactor module exporting the
// proxy-wasm ABI functions the proxy-wasm-go-sdk relies on.
func BuildWasm(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	destWasm := LocalCacheExtension(dirs, manifest)

	cacheDir := LocalCacheExtensionDir(dirs, manifest)
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	// #nosec G204
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", destWasm, ".")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Debug("building wasm module", "cmd", cmd.String(), "output", destWasm)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build wasm module from %s: %w", path, err)
	}

	logger.Debug("wasm module built and cached", "path", destWasm)

	// Copy the main manifest and any sub-extension manifests to the cache directory.
	if err := copyExtensionManifests(path, cacheDir); err != nil {
		return fmt.Errorf("failed to copy extension manifests: %w", err)
	}

	return nil
}
