// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Client provides operations for pushing and pulling OCI artifacts.
type Client interface {
	// Push packages the directory at the given path and pushes it to the registry
	// with the specified tag. Returns the digest of the pushed artifact.
	Push(ctx context.Context, path, tag string) (string, error)

	// Pull fetches the artifact with the given tag from the registry and extracts
	// it to the specified destination path. Returns the digest of the pulled artifact.
	Pull(ctx context.Context, tag, destPath string) (string, error)
}

// Credentials holds authentication credentials for a registry.
type Credentials struct {
	Username string
	Password string
}

// client implements Client using an oras.Target for storage.
type client struct {
	target oras.Target
}

// NewClient creates a new Client for the specified target.
// The target can be any oras.Target implementation, such as a remote repository
// or an in-memory store.
func NewClient(target oras.Target) Client {
	return &client{target: target}
}

// RepositoryOptions configures a remote repository.
type RepositoryOptions struct {
	Credentials *Credentials
	PlainHTTP   bool
}

// NewRemoteRepository creates a new remote repository target for the specified reference.
// The reference should be in the format "registry/namespace/name"
// (e.g., "ghcr.io/myorg/myrepo").
func NewRemoteRepository(reference string, opts *RepositoryOptions) (*remote.Repository, error) {
	repo, err := remote.NewRepository(reference)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	if opts != nil {
		repo.PlainHTTP = opts.PlainHTTP

		if opts.Credentials != nil {
			repo.Client = &auth.Client{
				Client: retry.DefaultClient,
				Cache:  auth.NewCache(),
				Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{
					Username: opts.Credentials.Username,
					Password: opts.Credentials.Password,
				}),
			}
		}
	}

	return repo, nil
}

func (c *client) Push(ctx context.Context, path, tag string) (string, error) {
	// Package the directory
	pkgReader, err := PackageDirectory(path)
	if err != nil {
		return "", fmt.Errorf("failed to package directory: %w", err)
	}

	data, err := io.ReadAll(pkgReader)
	if err != nil {
		return "", fmt.Errorf("failed to read package data: %w", err)
	}

	// Create a temporary store for building the OCI package
	buildStore := memory.New()

	// Build the OCI package
	manifestDesc, err := BuildOCIPackage(ctx, buildStore, data)
	if err != nil {
		return "", fmt.Errorf("failed to build OCI package: %w", err)
	}

	// Tag the manifest in the build store
	if err = buildStore.Tag(ctx, manifestDesc, tag); err != nil {
		return "", fmt.Errorf("failed to tag manifest: %w", err)
	}

	// Copy from build store to target
	desc, err := oras.Copy(ctx, buildStore, tag, c.target, tag, oras.DefaultCopyOptions)
	if err != nil {
		return "", fmt.Errorf("failed to push to registry: %w", err)
	}

	return desc.Digest.String(), nil
}

func (c *client) Pull(ctx context.Context, tag, destPath string) (string, error) {
	// Create a memory store to pull into
	fetchStore := memory.New()

	// Copy from target to memory store
	desc, err := oras.Copy(ctx, c.target, tag, fetchStore, tag, oras.DefaultCopyOptions)
	if err != nil {
		return "", fmt.Errorf("failed to pull from registry: %w", err)
	}

	// Fetch the manifest
	manifestReader, err := fetchStore.Fetch(ctx, desc)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() { _ = manifestReader.Close() }()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(manifestReader).Decode(&manifest); err != nil {
		return "", fmt.Errorf("failed to decode manifest: %w", err)
	}

	// Extract each layer
	for _, layer := range manifest.Layers {
		layerReader, err := fetchStore.Fetch(ctx, layer)
		if err != nil {
			return "", fmt.Errorf("failed to fetch layer: %w", err)
		}

		if err := ExtractPackage(layerReader, destPath); err != nil {
			_ = layerReader.Close()
			return "", fmt.Errorf("failed to extract layer: %w", err)
		}
		_ = layerReader.Close()
	}

	return desc.Digest.String(), nil
}
