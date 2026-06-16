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
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

const devVersionTagSuffix = "-dev"

// Downloader represents an extension downloader with authentication options.
type Downloader struct {
	Logger *slog.Logger

	Registry              string
	Username              string
	Password              string
	Insecure              bool
	Dirs                  *xdg.Directories
	OS                    string
	Arch                  string
	DevVersions           bool // Whether to allow downloading dev versions (with -dev suffix). By default, only stable versions are allowed.
	DisableSourceFallback bool // Whether to disable fallback to download source artifact when platform-specific artifact is not found. By default, the fallback is enabled.

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
	Manifest       *Manifest // The extension manifest of the downloaded artifact.
	Path           string    // The local path where the extension artifact is downloaded.
	ArtifactType   string    // The artifact type of the extension (binary or source).
	ComposerBundle bool      // Whether the downloaded extension is a composer bundle (which may contain multiple extensions).
	// The specific extension's Manifest. One downloaded artifact may contain multiple extensions.
	ExtensionManifest *Manifest
}

// DownloadComposer downloads the composer from the specified repository and version into the downloadDir.
func (d *Downloader) DownloadComposer(ctx context.Context, version string, artifact string) (DownloadedExtension, error) {
	d.Logger.Info("downloading composer", "repository", d.Registry, "artifact", artifact, "version", version)
	ext, err := d.download(ctx, d.Registry+"/"+artifact, version, func(manifest *ocispec.Manifest) string {
		extensionManifest := ManifestFromOCI(manifest)
		if isSourceArtifact(manifest) {
			return LocalCacheExtensionSourceArtifactDir(d.Dirs, extensionManifest)
		}
		// use the composer version resolved from the manifest, as the input parameter one could be
		// "latest" which needs to be resolved to a concrete version.
		composerVersion := manifest.Annotations[OCIAnnotationComposerVersion]
		// composer-lite is an independent artifact cached in its own slot
		// (dym/composer-lite/<version>/libcomposer-lite.so).
		if artifact == ComposerArtifactLite {
			return LocalCacheComposerLiteDir(d.Dirs, composerVersion)
		}
		return LocalCacheComposerDir(d.Dirs, composerVersion)
	})
	if err != nil {
		return DownloadedExtension{}, err
	}

	// For backwards compatibility.
	if ext.ArtifactType == ArtifactBinary && artifact == ComposerArtifactLite {
		// Normalize legacy composer-lite binary artifacts that ship the loader as libcomposer.so
		// instead of libcomposer-lite.so.
		if ensureErr := ensureComposerLiteLib(d.Dirs, ext.Manifest.ComposerVersion); ensureErr != nil {
			return DownloadedExtension{}, ensureErr
		}
	}

	return ext, nil
}

// DownloadExtension downloads the extension from the specified repository and tag into the downloadDir.
// The bundle parameter is used to construct the OCI repository name for downloading the artifact.
// The name parameter is used to resolve the specific extension manifest within the downloaded artifact.
// For standalone extensions, bundle and name are the same. For bundled extensions (e.g. "composer/example-go"),
// the bundle is the parent artifact and the name is the child whose manifest must have a Parent field
// matching the bundle name.
func (d *Downloader) DownloadExtension(ctx context.Context, bundle, name, version string) (DownloadedExtension, error) {
	d.Logger.Info("downloading extension", "repository", d.Registry, "bundle", bundle, "extension", name, "version", version)
	repository := RepositoryName(d.Registry, bundle)

	artifact, err := d.download(ctx, repository, version, func(manifest *ocispec.Manifest) string {
		extensionManifest := ManifestFromOCI(manifest)
		if isSourceArtifact(manifest) {
			return LocalCacheExtensionSourceArtifactDir(d.Dirs, extensionManifest)
		}
		return LocalCacheExtensionDir(d.Dirs, extensionManifest)
	})
	if err != nil {
		return DownloadedExtension{}, err
	}

	// Mark the manifest as remote so that config generation knows the extension is
	// a remote one and can take it into account.
	artifact.Manifest.Remote = true

	extensionManifest, err := ResolveExtensionManifest(d.Dirs, artifact.Manifest, artifact.ArtifactType,
		name, d.Logger)
	if err != nil {
		return DownloadedExtension{}, fmt.Errorf("failed to resolve extension manifest for %q: %w", name, err)
	}

	// For bundled extensions (bundle != name), validate that the resolved extension
	// declares the bundle as its parent.
	if bundle != name {
		if extensionManifest.Parent == "" {
			return DownloadedExtension{},
				fmt.Errorf("extension %q in bundle %q has no parent set in its manifest", name, bundle)
		}
		if extensionManifest.Parent != bundle {
			return DownloadedExtension{},
				fmt.Errorf("extension %q declares parent %q but was requested from bundle %q",
					name, extensionManifest.Parent, bundle)
		}
	}

	extensionManifest.Remote = true
	artifact.ExtensionManifest = extensionManifest
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
	if errors.Is(err, oci.ErrPlatformNotFound) && !d.DisableSourceFallback {
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

	var rootManifest *Manifest
	rootManifestPath := filepath.Join(downloadDir, "manifest.yaml")
	if _, err := os.Stat(rootManifestPath); err == nil {
		d.Logger.Debug("downloaded artifact contains manifest.yaml at root, validating it", "path", rootManifestPath)
		rootManifest, err = LoadLocalManifest(rootManifestPath)
		if err != nil {
			return DownloadedExtension{}, fmt.Errorf("failed to validate manifest.yaml in downloaded artifact: %w", err)
		}
	}

	if rootManifest == nil {
		d.Logger.Warn("downloaded artifact does not contain a manifest.yaml at root, some metadata may be missing",
			"path", rootManifestPath)
		rootManifest = ManifestFromOCI(manifest)
	} else {
		// manifest.yaml won't carry the compile-time fields like CShared but it's important so we could
		// distinguish the Golang extensions and goplugin extensions.
		ociManifest := ManifestFromOCI(manifest)
		rootManifest.CShared = ociManifest.CShared
		if rootManifest.Version == "" {
			rootManifest.Version = ociManifest.Version
		}
		if rootManifest.ComposerVersion == "" {
			rootManifest.ComposerVersion = ociManifest.ComposerVersion
		}
	}

	return DownloadedExtension{
		Manifest:       rootManifest,
		Path:           downloadDir,
		ArtifactType:   manifest.Annotations[OCIAnnotationArtifact],
		ComposerBundle: isComposerSourceArtifact(manifest),
	}, nil
}

// isSourceArtifact checks if the downloaded artifact is a source artifact.
func isSourceArtifact(manifest *ocispec.Manifest) bool {
	return manifest.Annotations[OCIAnnotationArtifact] == ArtifactSource
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

// ResolveLatestComposerVersion queries the default OCI registry for the
// latest composer version tag, including dev versions.
func ResolveLatestComposerVersion(ctx context.Context, logger *slog.Logger) (string, error) {
	return resolveLatestComposerVersion(ctx, logger, newOCIRepositoryClient)
}

func resolveLatestComposerVersion(ctx context.Context, logger *slog.Logger,
	newClient func(*slog.Logger, string, string, string, bool) (oci.RepositoryClient, error),
) (string, error) {
	repository := DefaultOCIRegistry + "/" + ComposerArtifactLite
	client, err := newClient(logger, repository, "", "", false)
	if err != nil {
		return "", fmt.Errorf("failed to create OCI client for %q: %w", repository, err)
	}
	version, err := getLatestTag(ctx, client, repository, true)
	if err != nil {
		return "", fmt.Errorf("failed to resolve latest composer version: %w", err)
	}
	logger.Info("resolved composer version from registry", "version", version)
	return version, nil
}
