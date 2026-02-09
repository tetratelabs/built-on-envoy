// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestCreateWithDockerSupport(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := t.Context()

	// Create a local registry for testing
	container, registry, err := internaltesting.StartOCIRegistry(ctx)
	require.NoError(t, err, "failed to start local OCI registry")
	defer func() {
		if terminateError := container.Terminate(ctx); terminateError != nil {
			t.Logf("failed to terminate registry container: %v", terminateError)
		}
	}()

	// Wirte a custom buildkit.toml to the temp directory to ensure local registry is used
	buildkitConfig := fmt.Sprintf(`
[registry."%s"]
  http = true
  insecure = true
`, registry)
	err = os.WriteFile(filepath.Join(tmpDir, "buildkit.toml"), []byte(buildkitConfig), 0o600)
	require.NoError(t, err, "failed to write buildkit.toml")

	// Create a new builder instance that uses the custom buildkit configuration and host network.
	builderName := fmt.Sprintf("test-builder-%d", time.Now().Unix())
	// #nosec G204
	createBuilderCmd := exec.CommandContext(ctx, "docker", "buildx", "create",
		"--name", builderName,
		"--use",
		"--config", filepath.Join(tmpDir, "buildkit.toml"),
		"--driver-opt", "network=host",
	)
	output, err := createBuilderCmd.CombinedOutput()
	t.Logf("buildx create output: %s", string(output))
	require.NoError(t, err, "failed to create buildx builder")

	// Clean up after the test by removing the builder instance.
	defer func() {
		// #nosec G204
		destroyCmd := exec.CommandContext(ctx, "docker", "buildx", "rm", builderName)
		output, destroyBuilderErr := destroyCmd.CombinedOutput()
		t.Logf("buildx rm output: %s", string(output))
		if destroyBuilderErr != nil {
			t.Logf("failed to remove buildx builder: %v", destroyBuilderErr)
		}
	}()

	// Create a new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "test-docker", "--path", tmpDir)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	extensionDir := filepath.Join(tmpDir, "test-docker")

	// Verify all required files were created
	requiredFiles := []string{
		"plugin.go",
		"manifest.yaml",
		"Makefile",
		"go.mod",
		"Dockerfile",
		"Dockerfile.code",
		".dockerignore",
	}

	for _, file := range requiredFiles {
		filePath := filepath.Join(extensionDir, file)
		_, err := os.Stat(filePath)
		require.NoError(t, err, "file %s should exist", file)
	}

	t.Run("makefile_build_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "build")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make build output: %s", string(output))
		require.NoError(t, err, "Makefile build target should be valid")

		// List the extension directory after build to check for output files
		files, err := os.ReadDir(extensionDir)
		require.NoError(t, err, "Should be able to read extension directory after build")
		var fileNames []string
		for _, file := range files {
			fileNames = append(fileNames, file.Name())
		}
		t.Logf("Files in extension directory after build: %v", fileNames)

		// Check there is the `plugin.so` output file.
		_, fileCheckError := os.Stat(filepath.Join(extensionDir, "plugin.so"))
		if fileCheckError != nil {
			t.Errorf("Makefile build target should produce plugin.so, but got error: %v", fileCheckError)
		}
	})

	t.Run("makefile_image_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "build_image",
			fmt.Sprintf("OCI_REGISTRY=%s", registry))
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make build_image output: %s", string(output))
		require.NoError(t, err, "Makefile build_image target should be valid")

		// Push local image to registry and check its annotations
		// #nosec G204
		pushCmd := exec.CommandContext(ctx, "docker", "push",
			fmt.Sprintf("%s/built-on-envoy/extension-test-docker:0.0.1-linux-%s", registry, runtime.GOARCH))
		output, err = pushCmd.CombinedOutput()
		t.Logf("docker push output: %s", string(output))
		require.NoError(t, err, "Should be able to push image to local registry")

		// Pull the image manifest and check annotations
		// #nosec G204
		manifestCmd := exec.CommandContext(ctx, "docker", "buildx", "imagetools", "inspect",
			fmt.Sprintf("%s/built-on-envoy/extension-test-docker:0.0.1-linux-%s", registry, runtime.GOARCH),
			"--raw")
		output, err = manifestCmd.CombinedOutput()
		t.Logf("docker buildx imagetools inspect output: %s", string(output))
		require.NoError(t, err, "Should be able to inspect image with buildx imagetools")
	})

	t.Run("makefile_push_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_image",
			fmt.Sprintf("OCI_REGISTRY=%s", registry))
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_image output: %s", string(output))
		require.NoError(t, err, "Makefile push_image target should be valid")

		// Pull the image manifest and check annotations
		// #nosec G204
		manifestCmd := exec.CommandContext(ctx, "docker", "buildx", "imagetools", "inspect",
			fmt.Sprintf("%s/built-on-envoy/extension-test-docker:0.0.1", registry), "--raw")
		output, err = manifestCmd.CombinedOutput()
		t.Logf("docker buildx imagetools inspect output: %s", string(output))
		require.NoError(t, err, "Should be able to inspect image with buildx imagetools after push")
	})

	t.Run("makefile_code_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_code",
			fmt.Sprintf("OCI_REGISTRY=%s", registry))
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_code output: %s", string(output))
		require.NoError(t, err, "Makefile push_code target should be valid")

		// Pull the image manifest and check annotations
		// #nosec G204
		manifestCmd := exec.CommandContext(ctx, "docker", "buildx", "imagetools", "inspect",
			fmt.Sprintf("%s/built-on-envoy/extension-src-test-docker:0.0.1", registry), "--raw")
		output, err = manifestCmd.CombinedOutput()
		t.Logf("docker buildx imagetools inspect output for code image: %s", string(output))
		require.NoError(t, err, "Should be able to inspect code image with buildx imagetools after push")
	})
}
