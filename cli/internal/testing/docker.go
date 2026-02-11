// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// CreateBuildxBuilder creates a new Docker Buildx builder instance with host network configuration for testing.
func CreateBuildxBuilder(t *testing.T) {
	// Create a new builder instance that uses the custom buildkit configuration and host network.
	builderName := fmt.Sprintf("test-builder-%d", time.Now().Unix())
	// #nosec G204
	createBuilderCmd := exec.CommandContext(t.Context(), "docker", "buildx", "create",
		"--name", builderName,
		"--use",
		"--driver-opt", "network=host",
	)
	output, err := createBuilderCmd.CombinedOutput()
	t.Logf("buildx create output: %s", string(output))
	require.NoError(t, err, "failed to create buildx builder")

	// Clean up after the test by removing the builder instance.
	t.Cleanup(func() {
		// #nosec G204
		// Do not use t.Context() inside Cleanup functions!
		destroyCmd := exec.CommandContext(context.Background(), "docker", "buildx", "rm", builderName)
		output, destroyBuilderErr := destroyCmd.CombinedOutput()
		t.Logf("buildx rm output: %s", string(output))
		if destroyBuilderErr != nil {
			t.Logf("failed to remove buildx builder: %v", destroyBuilderErr)
		}
	})
}
