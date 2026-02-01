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
	repoClientTests = append(repoClientTests, repoClientTest{
		name:          "docker registry",
		newRepoClient: newLocalRegistryRepositoryClient,
	})
}

func newLocalRegistryRepositoryClient(t *testing.T) RepositoryClient {
	// Create a remote repository and client
	repoRef := fmt.Sprintf("%s/test/extension", registryAddr)
	repo, err := NewRemoteRepository(repoRef, &ClientOptions{
		PlainHTTP: true, // Local registry doesn't use TLS
	})
	require.NoError(t, err)

	return NewRepositoryClient(repo)
}
