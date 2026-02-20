// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// LibGoBundleVersion is the version of the go_bundle extension used in the current build.
// The value is automatically generated in the code-generation step from the build process
// implemented in the `sync-manifests.sh` script.
// The version is extracted from the `libgobundle` Makefile.
//
//go:embed manifests/libgobundle-version.txt
var LibGoBundleVersion string

// CheckOrDownloadLibGoBundle checks if the libgobundle.so exists in the local cache directory.
// If not, it tries to download the pre-built libgobundle from OCI registry.
func CheckOrDownloadLibGoBundle(ctx context.Context, downloader *Downloader, version string) error {
	if _, err := os.Stat(LocalCacheGoBundleLib(downloader.Dirs, version)); err == nil {
		downloader.Logger.Debug("libgobundle already exists in local cache. skipping download", "version", version)
		return nil
	}
	artifact, err := downloader.DownloadGoBundle(ctx, version)
	if err != nil {
		return fmt.Errorf("failed to download libgobundle: %w", err)
	}

	// If the downloaded artifact is a binary, we are done. If it's a source artifact, we need to build it.
	if artifact.ArtifactType == ArtifactBinary {
		return nil
	}

	return BuildLibGoBundle(downloader.Logger, downloader.Dirs, artifact.Path, version, false)
}

// BuildExtensionFromPath builds the extension plugin from the given path and saves it to
// the local cache directory for go_bundle to load.
func BuildExtensionFromPath(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	// Run go mod tidy in the local extension directory to ensure dependencies are up to date.
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = path
	logger.Debug("running 'go mod tidy' for local extension", "path", path, "cmd", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run 'go mod tidy' in %s: %w\nOutput: %s",
			path, err, string(output))
	}

	// Build the extension and save the binary in the local cache directory for go_bundle to load.
	dest := LocalCacheExtension(dirs, manifest)
	// #nosec G204
	cmd = exec.Command("go", "build", "-buildmode=plugin", "-o", dest, "./standalone")
	cmd.Dir = path
	logger.Debug("building local extension", "version", manifest.Version, "path", path, "cmd", cmd.String())
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build local extension from %s: %w\nOutput: %s",
			path, err, string(output))
	}

	return nil
}

// BuildLibGoBundle builds the libgobundle.so from source. The go_bundle source code is expected
// to be at gobundleSrcPath. The built libgobundle.so will be saved in the local cache directory for
// go_bundle to load.
func BuildLibGoBundle(logger *slog.Logger, dirs *xdg.Directories, gobundleSrcPath string, version string, buildPlugins bool) error {
	if _, err := os.Stat(LocalCacheGoBundleLib(dirs, version)); err == nil {
		logger.Debug("libgobundle already exists in local cache. skipping build", "version", version)
		return nil
	}

	// Build the libgobundle from source.
	// #nosec G204
	cmd := exec.Command("make",
		"install",
		"BOE_DATA_HOME="+dirs.DataHome,
		"GOBUNDLE_LITE=true",
	)
	cmd.Dir = gobundleSrcPath

	logger.Debug("building libgobundle from source", "version", version, "cmd", cmd.String())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build libgobundle from source at %s: %w\nOutput: %s",
			gobundleSrcPath, err, string(output))
	}

	if buildPlugins {
		// #nosec G204
		pluginsBuild := exec.Command("make",
			"install_plugins",
			"BOE_DATA_HOME="+dirs.DataHome,
		)
		pluginsBuild.Dir = gobundleSrcPath

		logger.Debug("building go_bundle plugins from source", "cmd", pluginsBuild.String())

		output, err = pluginsBuild.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to build go_bundle example plugin from source at %s: %w\nOutput: %s",
				gobundleSrcPath, err, string(output))
		}
	}

	return nil
}
