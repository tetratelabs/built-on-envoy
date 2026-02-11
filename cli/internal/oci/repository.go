// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/mod/semver"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
)

// RepositoryClient provides operations for pushing and pulling OCI artifacts.
type RepositoryClient interface {
	// Push packages the directory at the given path and pushes it to the registry
	// with the specified tag. Returns the digest of the pushed artifact.
	Push(ctx context.Context, path, tag string, annotations map[string]string) (string, error)
	// Pull fetches the artifact with the given tag from the registry and extracts
	// it to the specified destination path. Returns the digest of the pulled artifact.
	Pull(ctx context.Context, tag, destPath string, platform *ocispec.Platform) (ocispec.Manifest, string, error)
	// Tags lists all tags in the repository.
	Tags(ctx context.Context) ([]string, error)
	// FetchManifest retrieves the manifest for the specified tag.
	FetchManifest(ctx context.Context, tag string, platform *ocispec.Platform) (ocispec.Manifest, error)
}

// TargetWithTags extends oras.Target to support listing tags.
type TargetWithTags interface {
	oras.Target
	// Tags lists all tags in the target.
	Tags(ctx context.Context, last string, fn func(tags []string) error) error
}

// repositoryClient implements RepositoryClient using an oras.Target for storage.
type repositoryClient struct {
	target TargetWithTags
}

// NewRepositoryClient creates a new RepositoryClient for the specified target.
// The target can be any oras.Target implementation, such as a remote repository
// or an in-memory store.
func NewRepositoryClient(target TargetWithTags) RepositoryClient {
	return &repositoryClient{target: target}
}

// NewRemoteRepository creates a new remote repository target for the specified reference.
// The reference should be in the format "registry/namespace/name"
// (e.g., "ghcr.io/myorg/myrepo").
func NewRemoteRepository(reference string, opts *ClientOptions) (*remote.Repository, error) {
	repo, err := remote.NewRepository(reference)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	repo.Client = newRemoteClient(repo.Reference.Registry, opts)
	if opts != nil {
		repo.PlainHTTP = opts.PlainHTTP
	}
	return repo, nil
}

func (r *repositoryClient) Push(ctx context.Context, path, tag string, annotations map[string]string) (string, error) {
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
	manifestDesc, err := BuildOCIPackage(ctx, buildStore, data, annotations)
	if err != nil {
		return "", fmt.Errorf("failed to build OCI package: %w", err)
	}

	// Tag the manifest in the build store
	if err = buildStore.Tag(ctx, manifestDesc, tag); err != nil {
		return "", fmt.Errorf("failed to tag manifest: %w", err)
	}

	// Copy from build store to target
	desc, err := oras.Copy(ctx, buildStore, tag, r.target, tag, oras.DefaultCopyOptions)
	if err != nil {
		return "", fmt.Errorf("failed to push to registry: %w", err)
	}

	return desc.Digest.String(), nil
}

func (r *repositoryClient) Pull(ctx context.Context, tag, destPath string, platform *ocispec.Platform) (ocispec.Manifest, string, error) {
	manifest, desc, err := r.fetchManifest(ctx, tag, platform)
	if err != nil {
		return ocispec.Manifest{}, "", fmt.Errorf("failed to get manifest: %w", err)
	}

	// Extract each layer
	for _, layer := range manifest.Layers {
		layerReader, err := r.target.Fetch(ctx, layer)
		if err != nil {
			return ocispec.Manifest{}, "", fmt.Errorf("failed to fetch layer: %w", err)
		}

		if err := ExtractPackage(layerReader, destPath); err != nil {
			_ = layerReader.Close()
			return ocispec.Manifest{}, "", fmt.Errorf("failed to extract layer: %w", err)
		}
		_ = layerReader.Close()
	}

	return manifest, desc.Digest.String(), nil
}

// ErrPlatformNotFound is returned when no manifest is found for the specified platform in a multi-arch artifact.
var ErrPlatformNotFound = errors.New("no manifest found for specified platform")

// fetchManifest retrieves and decodes the manifest for the specified tag.
func (r *repositoryClient) fetchManifest(ctx context.Context, tag string, platform *ocispec.Platform) (ocispec.Manifest, ocispec.Descriptor, error) {
	desc, err := r.target.Resolve(ctx, tag)
	if err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("failed to resolve tag: %w", err)
	}

	// If it is an index (multi-arch artifact), fetch it to find the right manifest for the current platform
	if platform != nil && desc.MediaType == ocispec.MediaTypeImageIndex {
		var rc io.ReadCloser
		rc, err = r.target.Fetch(ctx, desc)
		if err != nil {
			return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("failed to fetch index: %w", err)
		}
		defer func() { _ = rc.Close() }()

		var indexBytes []byte
		indexBytes, err = io.ReadAll(rc)
		if err != nil {
			return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("failed to read index: %w", err)
		}

		var index ocispec.Index
		if err = json.Unmarshal(indexBytes, &index); err != nil {
			return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("failed to unmarshal index: %w", err)
		}

		found := false
		for _, m := range index.Manifests {
			if m.Platform != nil && m.Platform.OS == platform.OS && m.Platform.Architecture == platform.Architecture {
				desc = m
				found = true
				break
			}
		}

		if !found {
			return ocispec.Manifest{}, ocispec.Descriptor{},
				fmt.Errorf("%w: %s/%s", ErrPlatformNotFound, platform.OS, platform.Architecture)
		}
	}

	// Fetch the manifest
	manifestReader, err := r.target.Fetch(ctx, desc)
	if err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() { _ = manifestReader.Close() }()

	var manifest ocispec.Manifest
	if err = json.NewDecoder(manifestReader).Decode(&manifest); err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return manifest, desc, nil
}

// Tags lists all tags in the repository.
func (r *repositoryClient) Tags(ctx context.Context) ([]string, error) {
	var tags []string
	err := r.target.Tags(ctx, "", func(t []string) error {
		tags = append(tags, t...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}

	// Sort by version, most recent first
	sort.Slice(tags, func(i, j int) bool {
		return semver.Compare("v"+tags[i], "v"+tags[j]) > 0
	})

	return tags, nil
}

// FetchManifest retrieves the manifest for the specified tag.
func (r *repositoryClient) FetchManifest(ctx context.Context, tag string, platform *ocispec.Platform) (ocispec.Manifest, error) {
	m, _, err := r.fetchManifest(ctx, tag, platform)
	return m, err
}
