// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"context"
	"fmt"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartOCIRegistry starts a local OCI registry for testing and returns
// the container instance and its address in "host:port" format.
func StartOCIRegistry(ctx context.Context) (testcontainers.Container, string, error) {
	registryContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "registry:2",
			ExposedPorts: []string{"5000/tcp"},
			WaitingFor:   wait.ForHTTP("/v2/").WithPort("5000/tcp"),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to start local Docker registry: %w", err)
	}

	host, err := registryContainer.Host(ctx)
	if err != nil {
		_ = registryContainer.Terminate(ctx)
		return nil, "", fmt.Errorf("failed to get registry host: %w", err)
	}
	port, err := registryContainer.MappedPort(ctx, "5000")
	if err != nil {
		_ = registryContainer.Terminate(ctx)
		return nil, "", fmt.Errorf("failed to get registry port: %w", err)
	}

	return registryContainer, fmt.Sprintf("%s:%s", host, port.Port()), nil
}
