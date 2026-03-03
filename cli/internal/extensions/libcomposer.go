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

// LibComposerVersion is the version of the composer extension used in the current build.
// The value is automatically generated in the code-generation step from the build process
// implemented in the `sync-manifests.sh` script.
// The version is extracted from the `libcomposer` Makefile.
//
//go:embed manifests/libcomposer-version.txt
var LibComposerVersion string

// CheckOrDownloadLibComposer checks if the libcomposer.so exists in the local cache directory.
// If not, it tries to download the pre-built libcomposer from OCI registry.
func CheckOrDownloadLibComposer(ctx context.Context, downloader *Downloader, version string, sourceArtifact bool) error {
	if _, err := os.Stat(LocalCacheComposerLib(downloader.Dirs, version)); err == nil {
		downloader.Logger.Debug("libcomposer already exists in local cache. skipping download", "version", version)
		return nil
	}
	return DownloadLibComposerAndBuildIfNeeded(ctx, downloader, version, sourceArtifact)
}

// DownloadLibComposerAndBuildIfNeeded is a helper function that combines downloading and building the libcomposer.
func DownloadLibComposerAndBuildIfNeeded(ctx context.Context, downloader *Downloader, version string, sourceArtifact bool) error {
	artifact, err := downloader.DownloadComposer(ctx, version, sourceArtifact)
	if err != nil {
		return fmt.Errorf("failed to download libcomposer: %w", err)
	}

	// If the downloaded artifact is a binary, we are done. If it's a source artifact, we need to build it.
	if artifact.ArtifactType == ArtifactBinary {
		return nil
	}

	return BuildLibComposer(downloader.Logger, downloader.Dirs, artifact.Path, version, false)
}

// BuildExtensionFromPath builds the extension plugin from the given path and saves it to
// the local cache directory for composer to load.
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

	// Build the extension and save the binary in the local cache directory for composer to load.
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

// BuildLibComposer builds the libcomposer.so from source. The composer source code is expected
// to be at composerSrcPath. The built libcomposer.so will be saved in the local cache directory for
// composer to load.
func BuildLibComposer(logger *slog.Logger, dirs *xdg.Directories, composerSrcPath string, version string, buildPlugins bool) error {
	// Build the libcomposer from source.
	// #nosec G204
	cmd := exec.Command("make",
		"install",
		"BOE_DATA_HOME="+dirs.DataHome,
		"COMPOSER_LITE=true",
	)
	cmd.Dir = composerSrcPath

	logger.Debug("building libcomposer from source", "version", version, "cmd", cmd.String())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build libcomposer from source at %s: %w\nOutput: %s",
			composerSrcPath, err, string(output))
	}

	if buildPlugins {
		// #nosec G204
		pluginsBuild := exec.Command("make",
			"install_plugins",
			"BOE_DATA_HOME="+dirs.DataHome,
		)
		pluginsBuild.Dir = composerSrcPath

		logger.Debug("building composer plugins from source", "cmd", pluginsBuild.String())

		output, err = pluginsBuild.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to build composer example plugin from source at %s: %w\nOutput: %s",
				composerSrcPath, err, string(output))
		}
	}

	return nil
}
