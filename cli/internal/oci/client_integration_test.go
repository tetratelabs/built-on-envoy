// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package oci

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func init() {
	clientTests = append(clientTests, clientTest{
		name:      "docker registry",
		newClient: newLocalRegistryClient,
	})
}

var registryAddr string

func TestMain(m *testing.M) {
	ctx := context.Background()
	registryContainer, addr, err := internaltesting.StartOCIRegistry(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start local OCI registry: %v\n", err)
		os.Exit(1)
	}
	registryAddr = addr

	code := m.Run()
	_ = registryContainer.Terminate(ctx)
	os.Exit(code)
}

func newLocalRegistryClient(t *testing.T) Client {
	// Create a remote repository and client
	repoRef := fmt.Sprintf("%s/test/extension", registryAddr)
	repo, err := NewRemoteRepository(repoRef, &RepositoryOptions{
		PlainHTTP: true, // Local registry doesn't use TLS
	})
	require.NoError(t, err)

	return NewClient(repo)
}
