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
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
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

	return buildLibComposer(dataHome, composerSrcPath)
}

func buildLibComposer(dataHome string, composerSrcPath string) error {
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

	// #nosec G204
	exampleCmd := exec.Command("make", "-C",
		"composer",
		"install_plugins",
		"BOE_DATA_HOME="+dataHome,
	)
	exampleCmd.Dir = composerSrcPath

	exampleOutput, exampleErr := exampleCmd.CombinedOutput()
	if exampleErr != nil {
		return fmt.Errorf("failed to build composer example plugin from source at %s: %w\nOutput: %s",
			composerSrcPath, exampleErr, string(exampleOutput))
	}

	return nil
}
