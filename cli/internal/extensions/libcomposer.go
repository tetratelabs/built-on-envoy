// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

const (
	// ComposerArtifact is the name of the embedded composer binary artifact in the OCI registry.
	ComposerArtifact = "composer"
	// ComposerArtifactLite is the name of composer (only go plugin) binary artifact in the OCI registry.
	ComposerArtifactLite = "composer-lite"
	// ComposerArtifactSource is the name of the composer source code artifact in the OCI registry.
	ComposerArtifactSource = "composer-src"
)

// CheckOrDownloadLibComposerLite checks if libcomposer-lite.so exists in the local cache directory.
// If not, it tries to download the pre-built composer-lite from the OCI registry (falling back to
// building it from source). composer-lite is the loader used to host standalone Go plugins.
func CheckOrDownloadLibComposerLite(ctx context.Context, downloader *Downloader, version string) error {
	if _, err := os.Stat(LocalCacheComposerLiteLib(downloader.Dirs, version)); err == nil {
		downloader.Logger.Debug("libcomposer-lite already exists in local cache. skipping download", "version", version)
		return nil
	}
	return DownloadComposerLiteAndBuildIfNeeded(ctx, downloader, version, ComposerArtifactLite)
}

// DownloadComposerLiteAndBuildIfNeeded combines downloading and building composer-lite: it pulls the
// given artifact (a prebuilt composer-lite binary, or composer-src) and, when source, builds
// libcomposer-lite.so from it.
func DownloadComposerLiteAndBuildIfNeeded(ctx context.Context, downloader *Downloader, version string, artifactName string) error {
	artifact, err := downloader.DownloadComposer(ctx, version, artifactName)
	if err != nil {
		return fmt.Errorf("failed to download libcomposer: %w", err)
	}

	// If the downloaded artifact is a binary, we are done. If it's a source artifact, we need to build it.
	if artifact.ArtifactType == ArtifactBinary {
		return nil
	}

	return BuildLibComposer(downloader.Logger, downloader.Dirs, artifact.Path, version, true)
}

// HasCSharedMain checks if the extension at the given path has a main/ directory,
// indicating it supports building as an independent c-shared library.
func HasCSharedMain(path string) bool {
	info, err := os.Stat(filepath.Join(path, "main"))
	return err == nil && info.IsDir()
}

// BuildExtensionFromPath builds the extension from the given path.
// If a main/ directory exists, it builds as a c-shared library (loaded directly by Envoy).
// Otherwise, it falls back to building as a Go plugin (loaded by composer/goplugin-loader).
func BuildExtensionFromPath(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) (cshared bool, err error) {
	// Run go mod tidy in the local extension directory to ensure dependencies are up to date.
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = path
	logger.Debug("running 'go mod tidy' for local extension", "path", path, "cmd", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to run 'go mod tidy' in %s: %w\nOutput: %s",
			path, err, string(output))
	}

	if HasCSharedMain(path) {
		return true, buildExtensionCShared(logger, dirs, manifest, path)
	}
	return false, buildExtensionPlugin(logger, dirs, manifest, path)
}

// buildExtensionCShared builds the extension as a c-shared library using the main/ directory.
func buildExtensionCShared(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	csharedManifest := *manifest
	csharedManifest.CShared = true

	dest := LocalCacheExtension(dirs, &csharedManifest)
	if err := os.MkdirAll(LocalCacheExtensionDir(dirs, &csharedManifest), 0o750); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}
	// #nosec G204
	cmd := exec.Command("go", "build", "-trimpath", "-buildmode=c-shared", "-o", dest, "./main")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	cmd.Dir = path
	logger.Debug("building local extension as c-shared", "version", manifest.Version, "path", path, "cmd", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build local extension from %s: %w\nOutput: %s",
			path, err, string(output))
	}
	return nil
}

// buildExtensionPlugin builds the extension as a Go plugin using the standalone/ directory.
func buildExtensionPlugin(logger *slog.Logger, dirs *xdg.Directories, manifest *Manifest, path string) error {
	dest := LocalCacheExtension(dirs, manifest)
	// #nosec G204
	cmd := exec.Command("go", "build", "-trimpath", "-buildmode=plugin", "-o", dest, "./standalone")
	cmd.Dir = path
	logger.Debug("building local extension as go-plugin", "version", manifest.Version, "path", path, "cmd", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build local extension from %s: %w\nOutput: %s",
			path, err, string(output))
	}
	return nil
}

// BuildLibComposer builds the libcomposer.so from source. The composer source code is expected
// to be at composerSrcPath. The built libcomposer.so will be saved in the local cache directory for
// composer to load.
func BuildLibComposer(logger *slog.Logger, dirs *xdg.Directories, composerSrcPath string, version string, lite bool) error {
	// composer-lite is built into its own independent cache slot (libcomposer-lite.so) so it
	// never collides with a full composer built from the same source for the same version.
	dest := LocalCacheComposerLib(dirs, version)
	if lite {
		dest = LocalCacheComposerLiteLib(dirs, version)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("failed to create composer cache directory: %w", err)
	}
	var buildTags string

	commonEnvPath := filepath.Join(composerSrcPath, "Makefile.common")
	if _, err := os.Stat(commonEnvPath); err == nil {
		env, err := godotenv.Read(commonEnvPath)
		if err != nil {
			logger.Warn("failed to read Makefile.common for build tags", "path", commonEnvPath, "error", err)
		}
		if tags, ok := env["BUILD_TAGS"]; ok {
			// The library supports reading `VAR=value` and `VAR: value`. The ':' is convenient as allows us to read Makefile style
			// variable declarations. We just need to cleanup the leading '='.
			buildTags = strings.TrimSpace(strings.TrimPrefix(tags, "="))
		}
	}
	if lite {
		if buildTags != "" {
			buildTags += ","
		}
		buildTags += "lite"
	}

	args := []string{
		"build",
		"-trimpath",
		"-buildmode=c-shared",
		"-o", dest,
	}
	if buildTags != "" {
		args = append(args, "-tags", buildTags)
	}
	args = append(args, "./main")

	// #nosec G204
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	cmd.Dir = composerSrcPath

	logger.Debug("building libcomposer from source", "version", version, "cmd", cmd.String())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build libcomposer from source at %s: %w\nOutput: %s",
			composerSrcPath, err, string(output))
	}

	return nil
}

// GetComposerManifest loads the composer manifest for the given version from the local cache,
// preferring the source-artifact manifest and falling back to the installed dynamic-module
// directory. It returns an error if no manifest is found for that version.
func GetComposerManifest(dirs *xdg.Directories, version string) (*Manifest, error) {
	// Prefer the source-artifact manifest, then the installed lite and full dynamic-module
	// directories (the runtime go-plugin host is composer-lite, so check it before composer).
	for _, dir := range []string{
		LocalCacheComposerSourceDir(dirs, version),
		LocalCacheComposerDir(dirs, version),
		LocalCacheComposerLiteDir(dirs, version),
	} {
		manifestPath := filepath.Join(dir, "manifest.yaml")
		if _, err := os.Stat(manifestPath); err == nil {
			return LoadLocalManifest(manifestPath)
		}
	}
	return nil, fmt.Errorf("manifest.yaml not found for composer version %s", version)
}
