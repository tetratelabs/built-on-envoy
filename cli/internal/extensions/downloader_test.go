// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// mockRepositoryClient is a mock implementation of oci.RepositoryClient for testing.
type mockRepositoryClient struct {
	repo                string
	manifestAnnotations map[string]string
	fetchErr            error
	tags                []string
	tagsErr             error
	pullDigest          string
	pullErr             error
}

func (m *mockRepositoryClient) Push(context.Context, string, string, map[string]string) (string, error) {
	return "", nil
}

func (m *mockRepositoryClient) Pull(context.Context, string, string, *ocispec.Platform) (*ocispec.Manifest, string, error) {
	return nil, m.pullDigest, m.pullErr
}

func (m *mockRepositoryClient) Tags(context.Context) ([]string, error) {
	return m.tags, m.tagsErr
}

func (m *mockRepositoryClient) FetchManifest(_ context.Context, tag string, _ *ocispec.Platform) (*ocispec.Manifest, error) {
	if m.fetchErr != nil {
		manifest := &ocispec.Manifest{}
		if m.manifestAnnotations != nil {
			manifest.Annotations = m.manifestAnnotations
		}
		return manifest, m.fetchErr
	}
	if m.manifestAnnotations != nil {
		return &ocispec.Manifest{Annotations: m.manifestAnnotations}, nil
	}
	return &ocispec.Manifest{
		Annotations: map[string]string{
			ocispec.AnnotationTitle:    m.repo,
			ocispec.AnnotationVersion:  tag,
			OCIAnnotationExtensionType: "dynamic_module",
		},
	}, nil
}

func TestGetLatestTag(t *testing.T) {
	errNetwork := errors.New("network error")

	tests := []struct {
		name     string
		tags     []string
		allowDev bool
		tagsErr  error
		wantTag  string
		wantErr  error
	}{
		{
			name:    "returns first tag from sorted list does not allow dev",
			tags:    []string{"3.0.0-dev", "2.0.0", "1.5.0", "1.0.0"},
			wantTag: "2.0.0",
		},
		{
			name:     "returns first tag from sorted list allows dev",
			tags:     []string{"3.0.0-dev", "2.0.0", "1.5.0", "1.0.0"},
			allowDev: true,
			wantTag:  "3.0.0-dev",
		},
		{
			name:    "empty tags list",
			tags:    []string{},
			wantErr: errNoTags,
		},
		{
			name:    "nil tags list",
			tags:    nil,
			wantErr: errNoTags,
		},
		{
			name:    "error fetching tags",
			tagsErr: errNetwork,
			wantErr: errNetwork,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockRepositoryClient{
				tags:    tt.tags,
				tagsErr: tt.tagsErr,
			}

			tag, err := getLatestTag(t.Context(), client, "test-repo", tt.allowDev)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestDownloadExtension(t *testing.T) {
	errPull := errors.New("pull error")
	errClient := errors.New("client creation error")

	dirs := &xdg.Directories{DataHome: t.TempDir()}

	tests := []struct {
		name        string
		version     string
		mock        *mockRepositoryClient
		clientErr   error
		wantName    string
		wantVersion string
		wantErr     error
	}{
		{
			name:    "download with specific version",
			version: "1.0.0",
			mock: &mockRepositoryClient{
				repo:       "myext",
				pullDigest: "sha256:abc123",
			},
			wantName:    "myext",
			wantVersion: "1.0.0",
		},
		{
			name:    "download with latest resolves to newest tag",
			version: "latest",
			mock: &mockRepositoryClient{
				repo:       "myext",
				tags:       []string{"2.0.0", "1.0.0"},
				pullDigest: "sha256:def456",
			},
			wantName:    "myext",
			wantVersion: "2.0.0",
		},
		{
			name:    "download with latest fails when no tags",
			version: "latest",
			mock: &mockRepositoryClient{
				repo: "myext",
				tags: []string{},
			},
			wantErr: errNoTags,
		},
		{
			name:    "download fails on pull error",
			version: "1.0.0",
			mock: &mockRepositoryClient{
				repo:    "myext",
				pullErr: errPull,
			},
			wantErr: errPull,
		},
		{
			name:      "download fails on client creation error",
			version:   "1.0.0",
			mock:      &mockRepositoryClient{repo: "myext"},
			clientErr: errClient,
			wantErr:   errClient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Downloader{
				Logger:   internaltesting.NewTLogger(t),
				Registry: "ghcr.io/test",
				Dirs:     dirs,
				newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
					if tt.clientErr != nil {
						return nil, tt.clientErr
					}
					return tt.mock, nil
				},
			}

			artifact, err := d.DownloadExtension(t.Context(), "myext", "myext", tt.version)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				require.Equal(t, tt.wantName, artifact.Manifest.Name)
				require.Equal(t, tt.wantVersion, artifact.Manifest.Version)
				require.True(t, artifact.Manifest.Remote)
			}
		})
	}
}

func TestDownloadComposer(t *testing.T) {
	t.Run("binary composer", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		d := &Downloader{
			Logger:   internaltesting.NewTLogger(t),
			Registry: "ghcr.io/test",
			Dirs:     dirs,
			newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
				return &mockRepositoryClient{
					manifestAnnotations: map[string]string{
						ocispec.AnnotationTitle:      ComposerArtifactLite,
						ocispec.AnnotationVersion:    "0.5.0",
						OCIAnnotationComposerVersion: "0.5.0",
						OCIAnnotationExtensionType:   string(TypeComposer),
						OCIAnnotationArtifact:        ArtifactBinary,
					},
				}, nil
			},
		}

		artifact, err := d.DownloadComposer(t.Context(), "0.5.0", ComposerArtifactLite)
		require.NoError(t, err)
		require.Equal(t, ArtifactBinary, artifact.ArtifactType)
		require.False(t, artifact.ComposerBundle)
	})

	t.Run("source composer", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		d := &Downloader{
			Logger:   internaltesting.NewTLogger(t),
			Registry: "ghcr.io/test",
			Dirs:     dirs,
			newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
				return &mockRepositoryClient{
					manifestAnnotations: map[string]string{
						ocispec.AnnotationTitle:    ComposerArtifactSource,
						ocispec.AnnotationVersion:  "0.5.0",
						OCIAnnotationExtensionType: string(TypeComposer),
						OCIAnnotationArtifact:      ArtifactSource,
					},
				}, nil
			},
		}

		artifact, err := d.DownloadComposer(t.Context(), "0.5.0", ComposerArtifactSource)
		require.NoError(t, err)
		require.Equal(t, ArtifactSource, artifact.ArtifactType)
		require.True(t, artifact.ComposerBundle)
	})
}

func TestDownloadSourceFallback(t *testing.T) {
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	calls := 0
	d := &Downloader{
		Logger:   internaltesting.NewTLogger(t),
		Registry: "ghcr.io/test",
		Dirs:     dirs,
		OS:       "linux",
		Arch:     "arm64",
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			calls++
			if calls == 1 {
				// First call: binary repo returns platform not found.
				return &mockRepositoryClient{
					manifestAnnotations: map[string]string{
						ocispec.AnnotationTitle:    "myext",
						ocispec.AnnotationVersion:  "1.0.0",
						OCIAnnotationExtensionType: string(TypeRust),
						OCIAnnotationArtifact:      ArtifactBinary,
					},
					fetchErr: oci.ErrPlatformNotFound,
				}, nil
			}
			// Second call: source repo succeeds.
			return &mockRepositoryClient{
				manifestAnnotations: map[string]string{
					ocispec.AnnotationTitle:    "myext",
					ocispec.AnnotationVersion:  "1.0.0",
					OCIAnnotationExtensionType: string(TypeRust),
					OCIAnnotationArtifact:      ArtifactSource,
				},
			}, nil
		},
	}

	artifact, err := d.DownloadExtension(t.Context(), "myext", "myext", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, ArtifactSource, artifact.ArtifactType)
}

func TestDownloadSourceFallbackDisabled(t *testing.T) {
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	d := &Downloader{
		Logger:                internaltesting.NewTLogger(t),
		Registry:              "ghcr.io/test",
		Dirs:                  dirs,
		OS:                    "linux",
		Arch:                  "arm64",
		DisableSourceFallback: true,
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			return &mockRepositoryClient{
				manifestAnnotations: map[string]string{
					ocispec.AnnotationTitle:    "myext",
					ocispec.AnnotationVersion:  "1.0.0",
					OCIAnnotationExtensionType: string(TypeRust),
				},
				fetchErr: oci.ErrPlatformNotFound,
			}, nil
		},
	}

	_, err := d.DownloadExtension(t.Context(), "myext", "myext", "1.0.0")
	require.ErrorIs(t, err, oci.ErrPlatformNotFound)
}

func TestCheckOrDownloadLibComposerLiteCacheHit(t *testing.T) {
	version := "0.5.0"
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	require.NoError(t, os.MkdirAll(LocalCacheComposerLiteDir(dirs, version), 0o750))
	require.NoError(t, os.WriteFile(LocalCacheComposerLiteLib(dirs, version), []byte("cached"), 0o600))

	d := &Downloader{
		Logger:   internaltesting.NewTLogger(t),
		Registry: "ghcr.io/test",
		Dirs:     dirs,
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			t.Fatal("should not create client when cache hit")
			return nil, nil
		},
	}

	require.NoError(t, CheckOrDownloadLibComposerLite(t.Context(), d, version))
}

func TestDownloadComposerExtensionLoadsParentManifest(t *testing.T) {
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	m := &Manifest{Name: "parent-valid", Version: "1.0.0", Type: TypeGo}

	cacheDir := LocalCacheExtensionDir(dirs, m)
	cachedManifestPath := filepath.Join(cacheDir, "manifest.yaml")
	cachedManifest, err := os.ReadFile("testdata/parent_valid.yaml")
	require.NoError(t, err)

	// Create the files as if they were downloaded:
	// - mock extension binary
	// - manifest.yaml file as packaged by the old composer individual extensions
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	require.NoError(t, os.WriteFile(cachedManifestPath, cachedManifest, 0o600))
	require.NoError(t, os.WriteFile(LocalCacheExtension(dirs, m), []byte("cached"), 0o600))

	d := &Downloader{
		Logger:                internaltesting.NewTLogger(t),
		Registry:              "ghcr.io/test",
		Dirs:                  dirs,
		OS:                    "linux",
		Arch:                  "arm64",
		DisableSourceFallback: true,
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			return &mockRepositoryClient{
				manifestAnnotations: map[string]string{
					ocispec.AnnotationTitle:      "parent-valid",
					ocispec.AnnotationVersion:    "1.0.0",
					OCIAnnotationExtensionType:   string(TypeGo),
					OCIAnnotationComposerVersion: "1.0.0",
				},
			}, nil
		},
	}

	// backward-compatible behaviour for old individual composer extension packages.
	// the information of the manifest should be loaded without loading any further
	// information from any parent
	t.Run("no parent manifest", func(t *testing.T) {
		ext, err := d.DownloadExtension(t.Context(), "parent-valid", "parent-valid", "1.0.0")
		require.NoError(t, err)
		require.Equal(t, "Test Author", ext.Manifest.Author)
		require.Empty(t, ext.Manifest.MinEnvoyVersion)
		require.Empty(t, ext.Manifest.MaxEnvoyVersion)
	})

	// The information from the parent manifest should be loaded: ResolveExtensionManifest reads the
	// sibling manifest-composer.yaml and inherits the parent's Envoy constraints into the extension.
	t.Run("with parent manifest", func(t *testing.T) {
		// Create the parent manifest in the download location
		parentManifest, err := os.ReadFile("testdata/composer_test.yaml")
		require.NoError(t, err)
		parentManifestPath := filepath.Join(cacheDir, "manifest-composer.yaml")
		require.NoError(t, os.WriteFile(parentManifestPath, parentManifest, 0o600))

		ext, err := d.DownloadExtension(t.Context(), "parent-valid", "parent-valid", "1.0.0")
		require.NoError(t, err)
		require.Equal(t, "Test Author", ext.Manifest.Author)
		require.Equal(t, "1.38.0", ext.Manifest.MinEnvoyVersion)
		require.Equal(t, "1.39.0", ext.Manifest.MaxEnvoyVersion) // This is computed when loading based on the min version
	})
}

func TestDownloadBundleExtension(t *testing.T) {
	bundleRootManifest := []byte(`name: my-bundle
version: 1.0.0
composerVersion: 1.0.0
categories:
  - Network
author: Test Author
description: A Go extension bundle
longDescription: |
  A bundle that hosts multiple child extensions.
type: go
tags:
  - test
license: Apache-2.0
examples:
  - title: Basic usage
    description: Run the bundle
    code: |
      boe run --extension my-bundle
`)

	bundleOCIAnnotations := map[string]string{
		ocispec.AnnotationTitle:    "my-bundle",
		ocispec.AnnotationVersion:  "1.0.0",
		OCIAnnotationExtensionType: string(TypeGo),
		OCIAnnotationArtifact:      ArtifactBinary,
	}

	t.Run("child inherits version and composerVersion from bundle", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		bundleManifest := &Manifest{Name: "my-bundle", Version: "1.0.0", Type: TypeGo}
		cacheDir := LocalCacheExtensionDir(dirs, bundleManifest)
		require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "my-child"), 0o750))

		require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "manifest.yaml"), bundleRootManifest, 0o600))

		// Child manifest: parent is set, so version/composerVersion must be absent per schema.
		// They will be inherited from the root bundle manifest at resolve time.
		require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "my-child", "manifest.yaml"), []byte(`name: my-child
parent: my-bundle
categories:
  - Security
author: Test Author
description: A child extension that inherits parent version
longDescription: |
  This is a child extension.
type: go
tags:
  - test
license: Apache-2.0
examples: []
`), 0o600))

		d := &Downloader{
			Logger:   internaltesting.NewTLogger(t),
			Registry: "ghcr.io/test",
			Dirs:     dirs,
			newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
				return &mockRepositoryClient{manifestAnnotations: bundleOCIAnnotations}, nil
			},
		}

		artifact, err := d.DownloadExtension(t.Context(), "my-bundle", "my-child", "1.0.0")
		require.NoError(t, err)
		require.Equal(t, "my-child", artifact.ExtensionManifest.Name)
		require.Equal(t, "my-bundle", artifact.ExtensionManifest.Parent)
		require.True(t, artifact.ExtensionManifest.Remote)
		// Inherited from the root bundle manifest.
		require.Equal(t, "1.0.0", artifact.ExtensionManifest.Version)
		require.Equal(t, "1.0.0", artifact.ExtensionManifest.ComposerVersion)
	})

	t.Run("child with no parent field fails", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		bundleManifest := &Manifest{Name: "my-bundle", Version: "1.0.0", Type: TypeGo}
		cacheDir := LocalCacheExtensionDir(dirs, bundleManifest)
		require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "orphan"), 0o750))

		require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "manifest.yaml"), bundleRootManifest, 0o600))

		// Standalone child (no parent): has its own version/composerVersion.
		require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "orphan", "manifest.yaml"), []byte(`name: orphan
version: 1.0.0
composerVersion: 1.0.0
categories:
  - Network
author: Test Author
description: A standalone extension incorrectly placed in a bundle
longDescription: |
  This extension has no parent field set.
type: go
tags:
  - test
license: Apache-2.0
examples: []
`), 0o600))

		d := &Downloader{
			Logger:   internaltesting.NewTLogger(t),
			Registry: "ghcr.io/test",
			Dirs:     dirs,
			newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
				return &mockRepositoryClient{manifestAnnotations: bundleOCIAnnotations}, nil
			},
		}

		_, err := d.DownloadExtension(t.Context(), "my-bundle", "orphan", "1.0.0")
		require.Error(t, err)
		require.Contains(t, err.Error(), `extension "orphan" in bundle "my-bundle" has no parent set`)
	})

	t.Run("child declaring wrong parent fails", func(t *testing.T) {
		dirs := &xdg.Directories{DataHome: t.TempDir()}
		bundleManifest := &Manifest{Name: "my-bundle", Version: "1.0.0", Type: TypeGo}
		cacheDir := LocalCacheExtensionDir(dirs, bundleManifest)
		require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "misplaced"), 0o750))

		require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "manifest.yaml"), bundleRootManifest, 0o600))

		// Child declares a parent that doesn't match the bundle it was downloaded from.
		require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "misplaced", "manifest.yaml"), []byte(`name: misplaced
parent: other-bundle
categories:
  - Network
author: Test Author
description: A child that belongs to a different bundle
longDescription: |
  This extension declares the wrong parent.
type: go
tags:
  - test
license: Apache-2.0
examples: []
`), 0o600))

		d := &Downloader{
			Logger:   internaltesting.NewTLogger(t),
			Registry: "ghcr.io/test",
			Dirs:     dirs,
			newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
				return &mockRepositoryClient{manifestAnnotations: bundleOCIAnnotations}, nil
			},
		}

		_, err := d.DownloadExtension(t.Context(), "my-bundle", "misplaced", "1.0.0")
		require.Error(t, err)
		require.Contains(t, err.Error(), `extension "misplaced" declares parent "other-bundle" but was requested from bundle "my-bundle"`)
	})
}

func TestCheckOrDownloadLibComposerLiteCacheMiss(t *testing.T) {
	version := "0.5.0"
	dirs := &xdg.Directories{DataHome: t.TempDir()}

	d := &Downloader{
		Logger:   internaltesting.NewTLogger(t),
		Registry: "ghcr.io/test",
		Dirs:     dirs,
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			return &mockRepositoryClient{
				manifestAnnotations: map[string]string{
					ocispec.AnnotationTitle:      ComposerArtifactLite,
					ocispec.AnnotationVersion:    version,
					OCIAnnotationComposerVersion: version,
					OCIAnnotationExtensionType:   string(TypeComposer),
					OCIAnnotationArtifact:        ArtifactBinary,
				},
			}, nil
		},
	}

	require.NoError(t, CheckOrDownloadLibComposerLite(t.Context(), d, version))
}

func TestResolveLatestComposerVersion(t *testing.T) {
	tests := []struct {
		name        string
		tags        []string
		clientErr   error
		expected    string
		expectedErr string
	}{
		{
			name:     "resolves latest tag including dev",
			tags:     []string{"0.7.0-dev", "0.6.0"},
			expected: "0.7.0-dev",
		},
		{
			name:        "client creation error",
			clientErr:   errors.New("connection refused"),
			expectedErr: `failed to create OCI client for "ghcr.io/tetratelabs/built-on-envoy/composer-lite": connection refused`,
		},
		{
			name:        "no tags",
			tags:        []string{},
			expectedErr: "failed to resolve latest composer version: no tags found for repository: ghcr.io/tetratelabs/built-on-envoy/composer-lite",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version, err := resolveLatestComposerVersion(t.Context(), internaltesting.NewTLogger(t),
				func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
					if tc.clientErr != nil {
						return nil, tc.clientErr
					}
					return &mockRepositoryClient{tags: tc.tags}, nil
				})
			if tc.expectedErr != "" {
				require.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, version)
			}
		})
	}
}

func TestMergeManifestFromRoot(t *testing.T) {
	tests := []struct {
		name     string
		embedded *Manifest
		fromOCI  *Manifest
		expected *Manifest
	}{
		{
			name:     "fills empty version and composerVersion",
			embedded: &Manifest{Name: "ext", Type: TypeGo},
			fromOCI:  &Manifest{Version: "1.2.3", ComposerVersion: "0.5.0"},
			expected: &Manifest{Name: "ext", Type: TypeGo, Version: "1.2.3", ComposerVersion: "0.5.0"},
		},
		{
			name:     "preserves non-empty embedded values",
			embedded: &Manifest{Name: "ext", Type: TypeGo, Version: "2.0.0", ComposerVersion: "0.6.0"},
			fromOCI:  &Manifest{Version: "1.2.3", ComposerVersion: "0.5.0"},
			expected: &Manifest{Name: "ext", Type: TypeGo, Version: "2.0.0", ComposerVersion: "0.6.0"},
		},
		{
			name:     "fills only version when composerVersion is set",
			embedded: &Manifest{Name: "ext", Type: TypeGo, ComposerVersion: "0.6.0"},
			fromOCI:  &Manifest{Version: "1.2.3", ComposerVersion: "0.5.0"},
			expected: &Manifest{Name: "ext", Type: TypeGo, Version: "1.2.3", ComposerVersion: "0.6.0"},
		},
		{
			name:     "fills only composerVersion when version is set",
			embedded: &Manifest{Name: "ext", Type: TypeGo, Version: "2.0.0"},
			fromOCI:  &Manifest{Version: "1.2.3", ComposerVersion: "0.5.0"},
			expected: &Manifest{Name: "ext", Type: TypeGo, Version: "2.0.0", ComposerVersion: "0.5.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeManifestFromRoot(tt.embedded, tt.fromOCI)
			require.Equal(t, tt.expected, tt.embedded)
		})
	}
}
