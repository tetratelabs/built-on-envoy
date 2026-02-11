// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestCreateWithDockerSupport(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := t.Context()

	// Create a new builder instance that uses the custom buildkit configuration and host network.
	internaltesting.CreateBuildxBuilder(t)

	// Create a new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "test-docker", "--path", tmpDir)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	extensionDir := filepath.Join(tmpDir, "test-docker")
	version := extensions.LibComposerVersion

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
			fmt.Sprintf("OCI_REGISTRY=%s", registryAddr))
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make build_image output: %s", string(output))
		require.NoError(t, err, "Makefile build_image target should be valid")

		// Push local image to registry and check its annotations
		// #nosec G204
		pushCmd := exec.CommandContext(ctx, "docker", "push",
			fmt.Sprintf("%s/built-on-envoy/extension-test-docker:%s-linux-%s", registryAddr, version, runtime.GOARCH))
		output, err = pushCmd.CombinedOutput()
		t.Logf("docker push output: %s", string(output))
		require.NoError(t, err, "Should be able to push image to local registry")

		// Pull the image manifest and check annotations
		fetchManifest(t, registryAddr, "built-on-envoy/extension-test-docker", fmt.Sprintf("%s-linux-%s", version, runtime.GOARCH))
	})

	t.Run("makefile_push_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_image",
			fmt.Sprintf("OCI_REGISTRY=%s", registryAddr), "INSECURE_REGISTRY=true")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_image output: %s", string(output))
		require.NoError(t, err, "Makefile push_image target should be valid")

		// Pull the image manifest and check annotations
		fetchManifest(t, registryAddr, "built-on-envoy/extension-test-docker", version)
	})

	t.Run("makefile_code_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_code",
			fmt.Sprintf("OCI_REGISTRY=%s", registryAddr), "INSECURE_REGISTRY=true")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_code output: %s", string(output))
		require.NoError(t, err, "Makefile push_code target should be valid")

		// Pull the image manifest and check annotations
		fetchManifest(t, registryAddr, "built-on-envoy/extension-src-test-docker", version)
	})
}

func fetchManifest(t *testing.T, registry, repository, reference string) {
	url := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repository, reference)
	// Set Accept header to request OCI media types to ensure the manifest is in OCI format
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err, "failed to create HTTP request for manifest")
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err, "failed to get manifest from %s", url)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status code %d fetching manifest from %s: %s", resp.StatusCode, url, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read manifest body")
	t.Logf("Manifest for %s:%s: %s", repository, reference, string(body))
}
