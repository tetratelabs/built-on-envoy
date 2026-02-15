// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// rnd is a random number generator used for creating unique builder names in tests.
var rnd = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint: gosec

// CreateBuildxBuilder creates a new Docker Buildx builder instance with host network configuration for testing.
func CreateBuildxBuilder(ctx context.Context) (string, func(), error) {
	// Create a new builder instance that uses the custom buildkit configuration and host network.
	builderName := fmt.Sprintf("test-builder-%d", rnd.Int())
	// #nosec G204
	createBuilderCmd := exec.CommandContext(ctx, "docker", "buildx", "create",
		"--name", builderName,
		"--use",
		"--driver", "docker-container",
		"--driver-opt", "network=host",
	)
	if err := createBuilderCmd.Run(); err != nil {
		return "", nil, fmt.Errorf("failed to create buildx builder: %w", err)
	}

	// Return the builder name and the cleanup function
	return builderName, func() {
		// #nosec G204
		// Do not use t.Context() inside Cleanup functions!
		destroyCmd := exec.CommandContext(context.Background(), "docker", "buildx", "rm", builderName)
		_, _ = destroyCmd.CombinedOutput()
	}, nil
}

// CreateBuildxBuilderForTest is a helper function that creates a new Buildx builder and registers a cleanup function to remove it after the test completes.
func CreateBuildxBuilderForTest(t *testing.T) string {
	builderName, cleanup, err := CreateBuildxBuilder(t.Context())
	require.NoError(t, err)
	t.Cleanup(cleanup)
	return builderName
}
