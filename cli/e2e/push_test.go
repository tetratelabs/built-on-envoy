// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"io"
	"net/http"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/docker"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestPushLocalGoExtension(t *testing.T) {
	dataDir := t.TempDir()

	ctx := t.Context()

	checkDockerBuildxErr := docker.CheckDockerBuildx(ctx)
	if checkDockerBuildxErr != nil {
		t.Skipf("Skipping test because Docker Buildx is not available: %v", checkDockerBuildxErr)
	}

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "go-e2e", "--path", dataDir)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Start a local OCI registry
	container, registry, err := internaltesting.StartOCIRegistry(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	// Check host architecture to speed up the test to avoid qemu overhead
	// for unsupported architectures
	var platforms string
	switch runtime.GOARCH {
	case "amd64":
		platforms = "linux/amd64"
	default:
		platforms = "linux/arm64"
	}

	// Push the extension to the local registry
	process = internaltesting.RunCLI(t, cliBin, "push", dataDir+"/go-e2e",
		"--build",
		"--registry", registry+"/test",
		"--insecure",
		"--platforms", platforms,
	)
	status, err = process.Wait()
	require.NoError(t, err, "failed to push extension")
	require.Equal(t, 0, status.ExitCode())

	// Check if the image was pushed to the registry by inspecting the registry
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+registry+"/v2/test/extension-go-e2e/manifests/0.0.1", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "failed to query registry for pushed image")
	defer func() { _ = resp.Body.Close() }()

	output, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read registry response for pushed image")
	t.Logf("Registry response for pushed image: %s", string(output))
	// Check if the response contains the expected image reference
	require.Contains(t, string(output), `"org.opencontainers.image.description": "A custom Go extension."`)
}
