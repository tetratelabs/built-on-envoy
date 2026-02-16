// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	Pull(ctx context.Context, tag, destPath string, platform *ocispec.Platform) (*ocispec.Manifest, string, error)
	// Tags lists all tags in the repository.
	Tags(ctx context.Context) ([]string, error)
	// FetchManifest retrieves the manifest for the specified tag.
	FetchManifest(ctx context.Context, tag string, platform *ocispec.Platform) (*ocispec.Manifest, error)
}

// TargetWithTags extends oras.Target to support listing tags.
type TargetWithTags interface {
	oras.Target
	// Tags lists all tags in the target.
	Tags(ctx context.Context, last string, fn func(tags []string) error) error
}

// repositoryClient implements RepositoryClient using an oras.Target for storage.
type repositoryClient struct {
	logger *slog.Logger
	target TargetWithTags
}

// NewRepositoryClient creates a new RepositoryClient for the specified target.
// The target can be any oras.Target implementation, such as a remote repository
// or an in-memory store.
func NewRepositoryClient(logger *slog.Logger, target TargetWithTags) RepositoryClient {
	return &repositoryClient{logger: logger, target: target}
}

// NewRemoteRepository creates a new remote repository target for the specified reference.
// The reference should be in the format "registry/namespace/name"
// (e.g., "ghcr.io/myorg/myrepo").
func NewRemoteRepository(logger *slog.Logger, reference string, opts *ClientOptions) (*remote.Repository, error) {
	logger.Debug("creating remote repository client", "reference", reference)
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
	r.logger.Debug("pushing artifact to registry", "path", path, "tag", tag, "annotations", annotations)

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

func (r *repositoryClient) Pull(ctx context.Context, tag, destPath string, platform *ocispec.Platform) (*ocispec.Manifest, string, error) {
	r.logger.Debug("pulling artifact from registry", "tag", tag, "destPath", destPath, "platform", platformLogValue(platform))

	manifest, desc, err := r.fetchManifest(ctx, tag, platform)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get manifest: %w", err)
	}

	// Extract each layer
	for _, layer := range manifest.Layers {
		r.logger.Debug("extracting layer", "digest", layer.Digest.String(), "mediaType", layer.MediaType)
		layerReader, err := r.target.Fetch(ctx, layer)
		if err != nil {
			return nil, "", fmt.Errorf("failed to fetch layer: %w", err)
		}

		if err := ExtractPackage(layerReader, destPath); err != nil {
			_ = layerReader.Close()
			return nil, "", fmt.Errorf("failed to extract layer: %w", err)
		}
		_ = layerReader.Close()
	}

	return manifest, desc.Digest.String(), nil
}

// ErrPlatformNotFound is returned when no manifest is found for the specified platform in a multi-arch artifact.
var ErrPlatformNotFound = errors.New("no manifest found for specified platform")

// fetchManifest retrieves and decodes the manifest for the specified tag.
func (r *repositoryClient) fetchManifest(ctx context.Context, tag string, platform *ocispec.Platform) (*ocispec.Manifest, ocispec.Descriptor, error) {
	r.logger.Debug("resolving manifest for tag", "tag", tag, "platform", platformLogValue(platform))

	desc, err := r.target.Resolve(ctx, tag)
	if err != nil {
		return nil, ocispec.Descriptor{}, fmt.Errorf("failed to resolve tag: %w", err)
	}

	r.logger.Debug("resolved descriptor for tag", "digest", desc.Digest.String(), "mediaType", desc.MediaType)

	// If it is an index (multi-arch artifact), fetch it to find the right manifest for the current platform
	if platform != nil && desc.MediaType == ocispec.MediaTypeImageIndex {
		r.logger.Debug("descriptor is an index, finding the platform-specific manifest", "platform", platformLogValue(platform))

		var rc io.ReadCloser
		rc, err = r.target.Fetch(ctx, desc)
		if err != nil {
			return nil, ocispec.Descriptor{}, fmt.Errorf("failed to fetch index: %w", err)
		}
		defer func() { _ = rc.Close() }()

		var indexBytes []byte
		indexBytes, err = io.ReadAll(rc)
		if err != nil {
			return nil, ocispec.Descriptor{}, fmt.Errorf("failed to read index: %w", err)
		}

		var index ocispec.Index
		if err = json.Unmarshal(indexBytes, &index); err != nil {
			return nil, ocispec.Descriptor{}, fmt.Errorf("failed to unmarshal index: %w", err)
		}

		found := false
		for _, m := range index.Manifests {
			if m.Platform != nil && m.Platform.OS == platform.OS && m.Platform.Architecture == platform.Architecture {
				r.logger.Debug("found matching manifest in index", "digest", m.Digest.String(), "mediaType", m.MediaType)
				desc = m
				found = true
				break
			}
		}

		if !found {
			// If the manifest is not found, return the error but still return the index manifest, as it
			// will contain the annotations that can be used to fallback to download the source artifact.
			var manifest *ocispec.Manifest
			manifest, err = r.decodeManifest(ctx, &desc)
			if err != nil {
				return nil, ocispec.Descriptor{}, err
			}

			return manifest, ocispec.Descriptor{},
				fmt.Errorf("%w: %s", ErrPlatformNotFound, platformLogValue(platform))
		}
	}

	// Fetch the manifest
	manifest, err := r.decodeManifest(ctx, &desc)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	return manifest, desc, nil
}

// decodeManifest fetches and decodes the manifest for the given descriptor.
func (r *repositoryClient) decodeManifest(ctx context.Context, desc *ocispec.Descriptor) (*ocispec.Manifest, error) {
	manifestReader, err := r.target.Fetch(ctx, *desc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() { _ = manifestReader.Close() }()

	var manifest ocispec.Manifest
	if err = json.NewDecoder(manifestReader).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return &manifest, nil
}

// Tags lists all tags in the repository.
func (r *repositoryClient) Tags(ctx context.Context) ([]string, error) {
	r.logger.Debug("listing tags in repository")

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
func (r *repositoryClient) FetchManifest(ctx context.Context, tag string, platform *ocispec.Platform) (*ocispec.Manifest, error) {
	m, _, err := r.fetchManifest(ctx, tag, platform)
	return m, err
}

// platformLogValue formats the platform information for logging, handling nil values gracefully.
func platformLogValue(platform *ocispec.Platform) string {
	if platform == nil {
		return "any"
	}
	return cmp.Or(platform.OS, "-") + "/" + cmp.Or(platform.Architecture, "-")
}
