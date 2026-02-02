// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
)

// RegistryClient defines methods for interacting with an OCI registry.
type RegistryClient interface {
	// ListRepositories lists all repositories in the OCI registry.
	ListRepositories(ctx context.Context) ([]string, error)
}

// registryClient implements RegistryClient using an oras.Target for storage.
type registryClient struct {
	registry registry.Registry
}

// NewRegistryClient creates a new RegistryClient for the specified target.
// The target can be any oras.Target implementation, such as a remote registry
// or an in-memory store.
func NewRegistryClient(target registry.Registry) RegistryClient {
	return &registryClient{registry: target}
}

// NewRemoteRegistry creates a new remote registry target for the specified reference.
// The reference should be in the format "registry/namespace".
// (e.g., "ghcr.io/myorg").
func NewRemoteRegistry(reference string, opts *ClientOptions) (*remote.Registry, error) {
	registry, err := remote.NewRegistry(reference)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	registry.Client = newRemoteClient(registry.Reference.Registry, opts)
	if opts != nil {
		registry.PlainHTTP = opts.PlainHTTP
	}
	return registry, nil
}

// ListRepositories lists all repositories in the OCI registry.
func (r *registryClient) ListRepositories(ctx context.Context) ([]string, error) {
	return registry.Repositories(ctx, r.registry)
}
