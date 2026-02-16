// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package oci

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func init() {
	registryClientTests = append(registryClientTests, registryClientTest{
		name:              "docker registry",
		newRegistryClient: newLocalRegistryClient,
	})
}

func newLocalRegistryClient(t *testing.T) RegistryClient {
	logger := internaltesting.NewTLogger(t)
	srcDir := t.TempDir()

	// Populate the test repos
	for _, repoName := range testRepos {
		repoRef := fmt.Sprintf("%s/%s", registryAddr, repoName)
		repo, err := NewRemoteRepository(logger, repoRef, &ClientOptions{
			PlainHTTP: true, // Local registry doesn't use TLS
		})
		require.NoError(t, err)
		repoClient := NewRepositoryClient(logger, repo)

		_, err = repoClient.Push(t.Context(), srcDir, "test", nil)
		require.NoError(t, err)
	}

	// Create a remote registry and client
	registry, err := NewRemoteRegistry(logger, registryAddr, &ClientOptions{
		PlainHTTP: true, // Local registry doesn't use TLS
	})
	require.NoError(t, err)

	return NewRegistryClient(logger, registry)
}
