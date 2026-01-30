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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	clientTests = append(clientTests, clientTest{
		name:      "docker registry",
		newClient: newLocalRegistryClient,
	})
}

var (
	registryContainer testcontainers.Container
	registryAddr      string
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start a local Docker registry
	var err error
	registryContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "registry:2",
			ExposedPorts: []string{"5000/tcp"},
			WaitingFor:   wait.ForHTTP("/v2/").WithPort("5000/tcp"),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to start local Docker registry: %v\n", err))
	}

	// Get the registry host
	host, err := registryContainer.Host(ctx)
	if err != nil {
		_ = registryContainer.Terminate(ctx)
		panic(fmt.Sprintf("Failed to get registry host: %v\n", err))
	}

	port, err := registryContainer.MappedPort(ctx, "5000")
	if err != nil {
		_ = registryContainer.Terminate(ctx)
		panic(fmt.Sprintf("Failed to get registry port: %v\n", err))
	}

	registryAddr = fmt.Sprintf("%s:%s", host, port.Port())
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
