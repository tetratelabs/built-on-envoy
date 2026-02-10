// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	"errors"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Downloader represents an extension downloader with authentication options.
type Downloader struct {
	Username string
	Password string
	Insecure bool
	Dirs     *xdg.Directories
	OS       string
	Arch     string

	// client factory function to allow mocking in tests.
	newClient func(repository, username, password string, insecure bool) (oci.RepositoryClient, error)
}

// Download downloads the extension from the specified repository and tag into the downloadDir.
func (d *Downloader) Download(ctx context.Context, repository, version, path string) (string, string, error) {
	if d.newClient == nil {
		d.newClient = newOCIRepositoryClient
	}

	client, err := d.newClient(repository, d.Username, d.Password, d.Insecure)
	if err != nil {
		return "", "", fmt.Errorf("failed to create OCI client for extension %q: %w", repository, err)
	}

	if version == "latest" {
		version, err = getLatestTag(ctx, client, repository)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve latest tag for extension %q: %w", repository, err)
		}
	}

	var platform *ocispec.Platform
	if d.OS != "" && d.Arch != "" {
		platform = &ocispec.Platform{
			OS:           d.OS,
			Architecture: d.Arch,
		}
	}

	downloadDir := downloadDirectory(path, d.Dirs, NameFromRepository(repository), version)
	_, digest, err := client.Pull(ctx, version, downloadDir, platform)
	return downloadDir, digest, err
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

// downloadDirectory sets the download directory if not provided by the user.
func downloadDirectory(base string, dirs *xdg.Directories, name, tag string) string {
	if base == "" {
		base = dirs.DataHome
	}
	return fmt.Sprintf("%s/extensions/%s/%s", base, name, tag)
}
