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
)

func init() {
	registryClientTests = append(registryClientTests, registryClientTest{
		name:              "docker registry",
		newRegistryClient: newLocalRegistryClient,
	})
}

func newLocalRegistryClient(t *testing.T) RegistryClient {
	// Populate the test repos
	srcDir := t.TempDir()

	// Populate the test repos
	for _, repoName := range testRepos {
		repoRef := fmt.Sprintf("%s/%s", registryAddr, repoName)
		repo, err := NewRemoteRepository(repoRef, &ClientOptions{
			PlainHTTP: true, // Local registry doesn't use TLS
		})
		require.NoError(t, err)
		repoClient := NewRepositoryClient(repo)

		_, err = repoClient.Push(t.Context(), srcDir, "test", nil)
		require.NoError(t, err)
	}

	// Create a remote registry and client
	registry, err := NewRemoteRegistry(registryAddr, &ClientOptions{
		PlainHTTP: true, // Local registry doesn't use TLS
	})
	require.NoError(t, err)

	return NewRegistryClient(registry)
}
