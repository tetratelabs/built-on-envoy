// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	"errors"
	"fmt"
	"os"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Downloader represents an extension downloader with authentication options.
type Downloader struct {
	Registry string
	Username string
	Password string
	Insecure bool
	Dirs     *xdg.Directories
	OS       string
	Arch     string

	// client factory function to allow mocking in tests.
	newClient func(repository, username, password string, insecure bool) (oci.RepositoryClient, error)
}

// DownloadComposer downloads the composer from the specified repository and version into the downloadDir.
func (d *Downloader) DownloadComposer(ctx context.Context, version string) error {
	return d.download(ctx, d.Registry+"/composer-lite", version, func(manifest *ocispec.Manifest) string {
		// use the composer version resolved from the manifest, as the input parameter one could be
		// "latest" which needs to be resolved to a concrete version.
		composerVersion := manifest.Annotations[OCIAnnotationComposerVersion]
		return LocalCacheComposerDir(d.Dirs, composerVersion, false)
	})
}

// DownloadExtension downloads the extension from the specified repository and tag into the downloadDir.
func (d *Downloader) DownloadExtension(ctx context.Context, name, version string) (*Manifest, error) {
	var (
		extensionManifest *Manifest
		downloadDir       string
		repository        = RepositoryName(d.Registry, name)
	)

	err := d.download(ctx, repository, version, func(manifest *ocispec.Manifest) string {
		extensionManifest = ManifestFromOCI(manifest)
		downloadDir = LocalCacheExtensionDir(d.Dirs, extensionManifest)
		return downloadDir
	})
	if err != nil {
		return nil, err
	}

	// If the Download dir contains the manifest (for lua extensions, for example), load it to get
	// the full manifest with all extension data.
	manifestPath := LocalCacheManifest(d.Dirs, extensionManifest)
	if _, err = os.Stat(manifestPath); err == nil {
		m, err := LoadLocalManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load manifest from downloaded extension at %q: %w", manifestPath, err)
		}
		extensionManifest = m
	}

	// Mark the manifest as remote so that config generation knows the extension is
	// a remote one and can take it into account.
	extensionManifest.Remote = true

	return extensionManifest, nil
}

// download the specified artifact using the provided downloader.
func (d *Downloader) download(
	ctx context.Context,
	repository string,
	version string,
	getDownloadDir func(*ocispec.Manifest) string,
) error {
	if d.newClient == nil {
		d.newClient = newOCIRepositoryClient
	}
	client, err := d.newClient(repository, d.Username, d.Password, d.Insecure)
	if err != nil {
		return fmt.Errorf("failed to create OCI client for %q: %w", repository, err)
	}

	if version == "latest" {
		version, err = getLatestTag(ctx, client, repository)
		if err != nil {
			return fmt.Errorf("failed to resolve latest tag for %q: %w", repository, err)
		}
	}

	var platform *ocispec.Platform
	if d.OS != "" && d.Arch != "" {
		platform = &ocispec.Platform{
			OS:           d.OS,
			Architecture: d.Arch,
		}
	}

	// Fetch the manifest first to read the annotations so that we can compute the right download directory
	manifest, err := client.FetchManifest(ctx, version, platform)
	if err != nil {
		return fmt.Errorf("failed to fetch manifest for %s:%s: %w", repository, version, err)
	}

	downloadDir := getDownloadDir(manifest)
	_, _, err = client.Pull(ctx, version, downloadDir, platform)
	return err
}

// newOCIRepositoryClient creates and assigns a new OCI client to the Push command.
func newOCIRepositoryClient(repository, username, password string, insecure bool) (oci.RepositoryClient, error) {
	opts := &oci.ClientOptions{PlainHTTP: insecure}
	if username != "" || password != "" {
		opts.Credentials = &oci.Credentials{
			Username: username,
			Password: password,
		}
	}

	// Instantiate the OCI client
	repo, err := oci.NewRemoteRepository(repository, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	return oci.NewRepositoryClient(repo), nil
}

var (
	errNoTags  = errors.New("no tags found for repository")
	errTagList = errors.New("failed to list tags for repository")
)

// getLatestTag retrieves the latest tag for the given repository.
func getLatestTag(ctx context.Context, client oci.RepositoryClient, repository string) (string, error) {
	tags, err := client.Tags(ctx)
	if err != nil {
		return "", fmt.Errorf("%w %q: %w", errTagList, repository, err)
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("%w: %s", errNoTags, repository)
	}
	// TThe client returns tags in descending order, according to SemVer.
	return tags[0], nil
}
