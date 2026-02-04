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
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// LibComposerVersion is the version of the composer extension used in the current build.
// The value is automatically generated in the code-generation step from the build process
// implemented in the `sync-manifests.sh` script.
// The version is extracted from the `libcomposer` Makefile.
//
//go:embed manifests/libcomposer-version.txt
var LibComposerVersion string

//go:embed extensions.tar
var ComposerExtenionsBytes []byte

func CheckOrBuildLibComposer(dataHome string) error {
	composerPath := filepath.Join(dataHome, "extensions", "dym", "composer",
		LibComposerVersion, "libcomposer.so")

	if _, err := os.Stat(composerPath); err == nil {
		// libcomposer already exists
		return nil
	}

	// Create temporary directory to extract the packaged extensions
	tempDir, err := os.MkdirTemp("/tmp", "boe-composer-ext")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Write the embedded tar to a temporary file
	tarPath := filepath.Join(tempDir, "extensions.tar")
	err = os.WriteFile(tarPath, ComposerExtenionsBytes, 0o640)
	if err != nil {
		return err
	}

	composerSrcPath := filepath.Join(tempDir, "extensions")

	// Extract the tar to the temporary directory
	err = os.MkdirAll(composerSrcPath, 0o750)
	if err != nil {
		return err
	}
	err = extractTar(tarPath, composerSrcPath)
	if err != nil {
		return err
	}

	return BuildLibComposer(dataHome, composerSrcPath)
}

func extractTar(tarPath, destDir string) error {
	// #nosec G204
	cmd := exec.Command("tar", "-xf", tarPath, "-C", destDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract tar %s to %s: %w\nOutput: %s",
			tarPath, destDir, err, string(output))
	}
	return nil
}

func BuildLibComposer(dataHome string, composerSrcPath string) error {
	// Build the libcomposer from source.

	// #nosec G204
	cmd := exec.Command("make", "-C",
		"internal/libcomposer",
		"build_local_cache",
		"BOE_DATA_HOME="+dataHome,
	)
	cmd.Dir = composerSrcPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build libcomposer from source at %s: %w\nOutput: %s",
			composerSrcPath, err, string(output))
	}
	return nil
}
