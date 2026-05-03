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

			artifact, err := d.DownloadExtension(t.Context(), "extension-myext", tt.version)
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

	artifact, err := d.DownloadExtension(t.Context(), "myext", "1.0.0")
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

	_, err := d.DownloadExtension(t.Context(), "myext", "1.0.0")
	require.ErrorIs(t, err, oci.ErrPlatformNotFound)
}

func TestCheckOrDownloadLibComposerCacheHit(t *testing.T) {
	version := "0.5.0"
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	require.NoError(t, os.MkdirAll(LocalCacheComposerDir(dirs, version), 0o750))
	require.NoError(t, os.WriteFile(LocalCacheComposerLib(dirs, version), []byte("cached"), 0o600))

	d := &Downloader{
		Logger:   internaltesting.NewTLogger(t),
		Registry: "ghcr.io/test",
		Dirs:     dirs,
		newClient: func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			t.Fatal("should not create client when cache hit")
			return nil, nil
		},
	}

	require.NoError(t, CheckOrDownloadLibComposer(t.Context(), d, version, ComposerArtifactLite))
}

func TestCheckOrDownloadLibComposerCacheMiss(t *testing.T) {
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

	require.NoError(t, CheckOrDownloadLibComposer(t.Context(), d, version, ComposerArtifactLite))
}

func TestMergeManifestFromOCI(t *testing.T) {
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
			mergeManifestFromOCI(tt.embedded, tt.fromOCI)
			require.Equal(t, tt.expected, tt.embedded)
		})
	}
}
