// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"context"
	"errors"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestDownloadDirectory(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}

	require.Equal(t, "/home/user/.local/share/extensions/test/1.0.1",
		downloadDirectory("", dirs, "test", "1.0.1"))
	require.Equal(t, "/custom/path/extensions/test/1.0.1",
		downloadDirectory("/custom/path", dirs, "test", "1.0.1"))
}

// mockRepositoryClient is a mock implementation of oci.RepositoryClient for testing.
type mockRepositoryClient struct {
	tags       []string
	tagsErr    error
	pullDigest string
	pullErr    error
}

func (m *mockRepositoryClient) Push(context.Context, string, string, map[string]string) (string, error) {
	return "", nil
}

func (m *mockRepositoryClient) Pull(context.Context, string, string, *ocispec.Platform) (ocispec.Manifest, string, error) {
	return ocispec.Manifest{}, m.pullDigest, m.pullErr
}

func (m *mockRepositoryClient) Tags(context.Context) ([]string, error) {
	return m.tags, m.tagsErr
}

func (m *mockRepositoryClient) FetchManifest(context.Context, string, *ocispec.Platform) (ocispec.Manifest, error) {
	return ocispec.Manifest{}, nil
}

func TestGetLatestTag(t *testing.T) {
	errNetwork := errors.New("network error")

	tests := []struct {
		name    string
		tags    []string
		tagsErr error
		wantTag string
		wantErr error
	}{
		{
			name:    "returns first tag from sorted list",
			tags:    []string{"2.0.0", "1.5.0", "1.0.0"},
			wantTag: "2.0.0",
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

			tag, err := getLatestTag(t.Context(), client, "test-repo")
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestDownload(t *testing.T) {
	errPull := errors.New("pull error")
	errClient := errors.New("client creation error")

	tests := []struct {
		name       string
		version    string
		path       string
		mock       *mockRepositoryClient
		clientErr  error
		wantDir    string
		wantDigest string
		wantErr    error
	}{
		{
			name:    "download with specific version",
			version: "1.0.0",
			path:    "/custom/path",
			mock: &mockRepositoryClient{
				pullDigest: "sha256:abc123",
			},
			wantDir:    "/custom/path/extensions/myext/1.0.0",
			wantDigest: "sha256:abc123",
		},
		{
			name:    "download with latest resolves to newest tag",
			version: "latest",
			path:    "/custom/path",
			mock: &mockRepositoryClient{
				tags:       []string{"2.0.0", "1.0.0"},
				pullDigest: "sha256:def456",
			},
			wantDir:    "/custom/path/extensions/myext/2.0.0",
			wantDigest: "sha256:def456",
		},
		{
			name:    "download with latest fails when no tags",
			version: "latest",
			mock: &mockRepositoryClient{
				tags: []string{},
			},
			wantErr: errNoTags,
		},
		{
			name:    "download fails on pull error",
			version: "1.0.0",
			path:    "/custom/path",
			mock: &mockRepositoryClient{
				pullErr: errPull,
			},
			wantDir: "/custom/path/extensions/myext/1.0.0",
			wantErr: errPull,
		},
		{
			name:      "download fails on client creation error",
			version:   "1.0.0",
			mock:      &mockRepositoryClient{},
			clientErr: errClient,
			wantErr:   errClient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Downloader{
				Dirs: &xdg.Directories{DataHome: "/default/data"},
				newClient: func(_, _, _ string, _ bool) (oci.RepositoryClient, error) {
					if tt.clientErr != nil {
						return nil, tt.clientErr
					}
					return tt.mock, nil
				},
			}

			dir, digest, err := d.Download(t.Context(), "ghcr.io/example/extension-myext", tt.version, tt.path)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantDir, dir)
			require.Equal(t, tt.wantDigest, digest)
		})
	}
}
