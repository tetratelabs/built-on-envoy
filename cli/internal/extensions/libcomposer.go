// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// TODO(wbpcode): remove this once we have a solution to distribute pre-built
// composer lib with the CLI binary.
// Synchronize the composer lib so we can build it at any machine.
//go:generate sh sync-composer.sh

package extensions

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// LibComposerVersion is the version of the composer extension used in the current build.
// The value is automatically generated in the code-generation step from the build process
// implemented in the `sync-manifests.sh` script.
// The version is extracted from the `libcomposer` Makefile.
//
//go:embed manifests/libcomposer-version.txt
var LibComposerVersion string

//go:embed extensions.tar.gz
var composerExtenionsBytes []byte

// CheckOrBuildLibComposer checks if the libcomposer.so exists in the dataHome directory.
// If not, it builds the libcomposer from source.
func CheckOrBuildLibComposer(dirs *xdg.Directories, buildPlugins bool) error {
	if _, err := os.Stat(LocalCacheComposerLib(dirs, LibComposerVersion)); err == nil {
		// libcomposer already exists
		return nil
	}

	// Create temporary directory to extract the packaged extensions
	tempDir, err := os.MkdirTemp("/tmp", "boe-composer-ext")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Write the embedded tar to a temporary file
	tarPath := filepath.Join(tempDir, "extensions.tar.gz")
	err = os.WriteFile(tarPath, composerExtenionsBytes, 0o600)
	if err != nil {
		return err
	}

	composerSrcPath := filepath.Join(tempDir, "extensions")

	// Create reader from the byte slice
	dataReader := bytes.NewReader(composerExtenionsBytes)
	err = oci.ExtractPackage(dataReader, composerSrcPath)
	if err != nil {
		return err
	}

	return buildLibComposer(dirs.DataHome, composerSrcPath, buildPlugins)
}

// CheckOrDownloadLibComposer checks if the libcomposer.so exists in the dataHome directory.
// If not, it tries to download the pre-built libcomposer from OCI registry.
func CheckOrDownloadLibComposer(ctx context.Context, downloader *Downloader, version string) error {
	if _, err := os.Stat(LocalCacheComposerLib(downloader.Dirs, version)); err == nil {
		// libcomposer already exists
		return nil
	}
	return downloader.DownloadComposer(ctx, version)
}

// BuildExtensionFromPath builds the extension plugin from the given path and saves it to
// the local cache directory for composer to load.
func BuildExtensionFromPath(dirs *xdg.Directories, manifest *Manifest, path string) error {
	// Run go mod tidy in the local extension directory to ensure dependencies are up to date.
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = path
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
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build local extension from %s: %w\nOutput: %s",
			path, err, string(output))
	}

	return nil
}

func buildLibComposer(dataHome string, composerSrcPath string, buildPlugins bool) error {
	// Build the libcomposer from source.

	// #nosec G204
	cmd := exec.Command("make", "-C",
		"composer",
		"install",
		"BOE_DATA_HOME="+dataHome,
	)
	cmd.Dir = composerSrcPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build libcomposer from source at %s: %w\nOutput: %s",
			composerSrcPath, err, string(output))
	}

	if buildPlugins {
		// #nosec G204
		pluginsDir := exec.Command("make", "-C",
			"composer",
			"install_plugins",
			"BOE_DATA_HOME="+dataHome,
		)
		pluginsDir.Dir = composerSrcPath

		output, err = pluginsDir.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to build composer example plugin from source at %s: %w\nOutput: %s",
				composerSrcPath, err, string(output))
		}
	}

	return nil
}
