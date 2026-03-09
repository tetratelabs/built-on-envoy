// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

const devVersionTagSuffix = "-dev"

// Downloader represents an extension downloader with authentication options.
type Downloader struct {
	Logger *slog.Logger

	Registry    string
	Username    string
	Password    string
	Insecure    bool
	Dirs        *xdg.Directories
	OS          string
	Arch        string
	DevVersions bool // Whether to allow downloading dev versions (with -dev suffix). By default, only stable versions are allowed.

	// client factory function to allow mocking in tests.
	newClient func(logger *slog.Logger, repository, username, password string, insecure bool) (oci.RepositoryClient, error)
}

// SetClientFactory sets a custom client factory function for the downloader.
// This is intended for use in tests to inject mock OCI clients.
func (d *Downloader) SetClientFactory(f func(logger *slog.Logger, repository, username, password string, insecure bool) (oci.RepositoryClient, error)) {
	d.newClient = f
}

// DownloadedExtension represents a downloaded extension with its manifest and local path.
type DownloadedExtension struct {
	Manifest       *Manifest // The extension manifest with all metadata.
	Path           string    // The local path where the extension artifact is downloaded.
	ArtifactType   string    // The artifact type of the extension (binary or source).
	ComposerBundle bool      // Whether the downloaded extension is a composer bundle (which may contain multiple extensions).
}

// DownloadComposer downloads the composer from the specified repository and version into the downloadDir.
func (d *Downloader) DownloadComposer(ctx context.Context, version string, sourceArtifact bool) (DownloadedExtension, error) {
	var artifact string
	if sourceArtifact {
		artifact = "composer-src"
	} else {
		artifact = "composer-lite"
	}
	d.Logger.Info("downloading composer", "repository", d.Registry, "artifact", artifact, "version", version)
	return d.download(ctx, d.Registry+"/"+artifact, version, func(manifest *ocispec.Manifest) string {
		extensionManifest := ManifestFromOCI(manifest)
		if isComposerSourceArtifact(manifest) {
			return LocalCacheComposerSourceArtifactDir(d.Dirs, extensionManifest)
		}
		// use the composer version resolved from the manifest, as the input parameter one could be
		// "latest" which needs to be resolved to a concrete version.
		composerVersion := manifest.Annotations[OCIAnnotationComposerVersion]
		return LocalCacheComposerDir(d.Dirs, composerVersion)
	})
}

// DownloadExtension downloads the extension from the specified repository and tag into the downloadDir.
func (d *Downloader) DownloadExtension(ctx context.Context, name, version string) (DownloadedExtension, error) {
	d.Logger.Info("downloading extension", "repository", d.Registry, "name", name, "version", version)
	repository := RepositoryName(d.Registry, name)

	artifact, err := d.download(ctx, repository, version, func(manifest *ocispec.Manifest) string {
		extensionManifest := ManifestFromOCI(manifest)
		if isComposerSourceArtifact(manifest) {
			return LocalCacheComposerSourceArtifactDir(d.Dirs, extensionManifest)
		}
		return LocalCacheExtensionDir(d.Dirs, extensionManifest)
	})
	if err != nil {
		return DownloadedExtension{}, err
	}

	// If the Download dir contains the manifest (lua extensinos or downloaded source), load it to get
	// the full manifest with all extension data.
	// Composer extensions are different as the manifest is the uber-manifest and we dont' want to read that.
	if !artifact.ComposerBundle {
		manifestPath := LocalCacheManifest(d.Dirs, artifact.Manifest)
		if _, err = os.Stat(manifestPath); err == nil {
			d.Logger.Info("loading manifest from downloaded extension", "path", manifestPath)
			m, err := LoadLocalManifest(manifestPath)
			if err != nil {
				return DownloadedExtension{},
					fmt.Errorf("failed to load manifest from downloaded extension at %q: %w", manifestPath, err)
			}
			artifact.Manifest = m
		}
	}

	// Mark the manifest as remote so that config generation knows the extension is
	// a remote one and can take it into account.
	artifact.Manifest.Remote = true

	return artifact, nil
}

// download the specified artifact using the provided downloader.
func (d *Downloader) download(
	ctx context.Context,
	repository string,
	version string,
	getDownloadDir func(*ocispec.Manifest) string,
) (DownloadedExtension, error) {
	if d.newClient == nil {
		d.newClient = newOCIRepositoryClient
	}
	client, err := d.newClient(d.Logger, repository, d.Username, d.Password, d.Insecure)
	if err != nil {
		return DownloadedExtension{}, fmt.Errorf("failed to create OCI client for %q: %w", repository, err)
	}

	if version == "latest" {
		version, err = getLatestTag(ctx, client, repository, d.DevVersions)
		if err != nil {
			return DownloadedExtension{}, fmt.Errorf("failed to resolve latest tag for %q: %w", repository, err)
		}
		d.Logger.Info("resolved latest tag for repository", "repository", repository, "tag", version)
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
	if errors.Is(err, oci.ErrPlatformNotFound) {
		// If the manifest for the specific platform is not found, we can fallback to download
		// the source artifact using the source repository.
		extensionName := NameFromRepository(repository)
		_, _ = fmt.Fprintf(os.Stderr, "No artifact found for %q for platform %s/%s, "+
			"falling back to download source artifact and building locally\n", extensionName, d.OS, d.Arch)

		extensionManifest := ManifestFromOCI(manifest)
		sourceRepo := SourceRepositoryName(d.Registry, extensionManifest)
		if extensionManifest.Type == TypeGo {
			version = extensionManifest.ComposerVersion
		}

		// Create a downloader for the source artifact without the platforms so it does not
		// try to fetch a multi-arch artifact.
		srcDownloader := &Downloader{
			Logger:      d.Logger,
			Registry:    d.Registry,
			Username:    d.Username,
			Password:    d.Password,
			Insecure:    d.Insecure,
			Dirs:        d.Dirs,
			DevVersions: d.DevVersions,
			newClient:   d.newClient,
		}

		return srcDownloader.download(ctx, sourceRepo, version, getDownloadDir)
	}
	if err != nil {
		return DownloadedExtension{}, fmt.Errorf("failed to fetch manifest for %s:%s: %w", repository, version, err)
	}

	downloadDir := getDownloadDir(manifest)
	_, _, err = client.Pull(ctx, version, downloadDir, platform)
	if err != nil {
		return DownloadedExtension{}, fmt.Errorf("failed to pull artifact for %s:%s: %w", repository, version, err)
	}

	return DownloadedExtension{
		Manifest:       ManifestFromOCI(manifest),
		Path:           downloadDir,
		ArtifactType:   manifest.Annotations[OCIAnnotationArtifact],
		ComposerBundle: isComposerSourceArtifact(manifest),
	}, nil
}

// isComposerSourceArtifact checks if the downloaded artifact is a composer source artifact.
func isComposerSourceArtifact(manifest *ocispec.Manifest) bool {
	return Type(manifest.Annotations[OCIAnnotationExtensionType]) == TypeComposer &&
		manifest.Annotations[OCIAnnotationArtifact] == ArtifactSource
}

// newOCIRepositoryClient creates and assigns a new OCI client to the Push command.
func newOCIRepositoryClient(logger *slog.Logger, repository, username, password string, insecure bool) (oci.RepositoryClient, error) {
	opts := &oci.ClientOptions{PlainHTTP: insecure}
	if username != "" || password != "" {
		opts.Credentials = &oci.Credentials{
			Username: username,
			Password: password,
		}
	}

	// Instantiate the OCI client
	repo, err := oci.NewRemoteRepository(logger, repository, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	return oci.NewRepositoryClient(logger, repo), nil
}

var (
	errNoTags  = errors.New("no tags found for repository")
	errTagList = errors.New("failed to list tags for repository")
)

// getLatestTag retrieves the latest tag for the given repository.
func getLatestTag(ctx context.Context, client oci.RepositoryClient, repository string, allowDevVersions bool) (string, error) {
	tags, err := client.Tags(ctx)
	if err != nil {
		return "", fmt.Errorf("%w %q: %w", errTagList, repository, err)
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("%w: %s", errNoTags, repository)
	}
	// The client returns tags in descending order, according to SemVer.
	// Return the first tag that is not a dev version unless allowDevVersions is true.
	for _, tag := range tags {
		if !allowDevVersions && strings.HasSuffix(tag, devVersionTagSuffix) {
			continue
		}
		return tag, nil
	}
	return "", fmt.Errorf("%w: %s", errNoTags, repository)
}
