// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
)

type repoClientTest struct {
	name          string
	newRepoClient func(t *testing.T) RepositoryClient
}

var repoClientTests = []repoClientTest{
	{
		name: "in-memory store",
		newRepoClient: func(*testing.T) RepositoryClient {
			return NewRepositoryClient(&memoryWithTags{
				Store: memory.New(),
			})
		},
	},
}

func TestRepositoryClient_PushPull(t *testing.T) {
	for _, tt := range repoClientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newRepoClient(t)

			// Create source directory with test files
			srcDir := t.TempDir()
			files := map[string]string{
				"file1.txt":        "content of file1",
				"subdir/file2.txt": "content of file2",
			}

			require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "subdir"), 0o750))
			for name, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o600))
			}

			// Push the directory
			tag := "v1.0.0"
			annotations := map[string]string{
				ocispec.AnnotationTitle:   "test-artifact",
				ocispec.AnnotationVersion: tag,
			}
			digest, err := client.Push(ctx, srcDir, tag, annotations)
			require.NoError(t, err)
			assert.NotEmpty(t, digest)
			assert.Contains(t, digest, "sha256:")

			// Pull to a new directory
			destDir := t.TempDir()
			manifest, pulledDigest, err := client.Pull(ctx, tag, destDir, nil)
			require.NoError(t, err)
			assert.Equal(t, digest, pulledDigest)
			assert.Equal(t, tag, manifest.Annotations[ocispec.AnnotationVersion])
			assert.Equal(t, "test-artifact", manifest.Annotations[ocispec.AnnotationTitle])
			assert.NotEmpty(t, manifest.Annotations[ocispec.AnnotationCreated])

			// Verify all files were extracted correctly
			for name, expectedContent := range files {
				var content []byte
				content, err = os.ReadFile(filepath.Clean(filepath.Join(destDir, name)))
				require.NoError(t, err, "failed to read file %s", name)
				assert.Equal(t, expectedContent, string(content), "content mismatch for %s", name)
			}

			// Check manifest
			fetchedManifest, err := client.FetchManifest(ctx, tag, nil)
			require.NoError(t, err)
			assert.Equal(t, manifest, fetchedManifest)
		})
	}
}

func TestRepositoryClient_PushPull_MultipleTags(t *testing.T) {
	for _, tt := range repoClientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newRepoClient(t)

			// Create and push first version
			srcDir1 := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(srcDir1, "version.txt"), []byte("v1"), 0o600))

			digest1, err := client.Push(ctx, srcDir1, "v1", nil)
			require.NoError(t, err)

			// Create and push second version
			srcDir2 := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(srcDir2, "version.txt"), []byte("v2"), 0o600))

			digest2, err := client.Push(ctx, srcDir2, "v2", nil)
			require.NoError(t, err)

			// Digests should be different
			assert.NotEqual(t, digest1, digest2)

			// Pull v1 and verify
			destDir1 := t.TempDir()
			manifest1, _, err := client.Pull(ctx, "v1", destDir1, nil)
			require.NoError(t, err)
			assert.NotEmpty(t, manifest1.Annotations[ocispec.AnnotationCreated])

			content1, err := os.ReadFile(filepath.Clean(filepath.Join(destDir1, "version.txt")))
			require.NoError(t, err)
			assert.Equal(t, "v1", string(content1))

			// Pull v2 and verify
			destDir2 := t.TempDir()
			manifest2, _, err := client.Pull(ctx, "v2", destDir2, nil)
			require.NoError(t, err)
			assert.NotEmpty(t, manifest2.Annotations[ocispec.AnnotationCreated])

			content2, err := os.ReadFile(filepath.Clean(filepath.Join(destDir2, "version.txt")))
			require.NoError(t, err)
			assert.Equal(t, "v2", string(content2))

			tags, err := client.Tags(ctx)
			require.NoError(t, err)
			require.Contains(t, tags, "v1")
			require.Contains(t, tags, "v2")
		})
	}
}

func TestRepositoryClient_Pull_NonExistentTag(t *testing.T) {
	for _, tt := range repoClientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newRepoClient(t)

			destDir := t.TempDir()
			_, _, err := client.Pull(ctx, "nonexistent", destDir, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to get manifest")
		})
	}
}

func TestRepositoryClient_Push_NonExistentPath(t *testing.T) {
	for _, tt := range repoClientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newRepoClient(t)

			_, err := client.Push(ctx, "/nonexistent/path", "v1", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no such file or directory")
		})
	}
}

func TestNewRemoteRepository(t *testing.T) {
	// Test creating a repository without options
	repo, err := NewRemoteRepository("ghcr.io/myorg/myrepo", nil)
	require.NoError(t, err)
	require.NotNil(t, repo)
	assert.Equal(t, "ghcr.io", repo.Reference.Registry)
	assert.Equal(t, "myorg/myrepo", repo.Reference.Repository)
}

func TestNewRemoteRepository_WithOptions(t *testing.T) {
	opts := &ClientOptions{
		Credentials: &Credentials{
			Username: "user",
			Password: "pass",
		},
		PlainHTTP: true,
	}

	repo, err := NewRemoteRepository("ghcr.io/myorg/myrepo", opts)
	require.NoError(t, err)
	require.NotNil(t, repo)
	assert.True(t, repo.PlainHTTP)
	assert.NotNil(t, repo.Client)
}

func TestNewRemoteRepository_InvalidReference(t *testing.T) {
	_, err := NewRemoteRepository("invalid:reference:format", nil)
	require.Error(t, err)
}

var _ TargetWithTags = (*memoryWithTags)(nil)

type memoryWithTags struct {
	*memory.Store
	tags []string
}

func (m *memoryWithTags) Tags(_ context.Context, _ string, fn func([]string) error) error {
	return fn(m.tags)
}

func (m *memoryWithTags) Tag(ctx context.Context, desc ocispec.Descriptor, reference string) error { //nolint:gocritic
	if err := m.Store.Tag(ctx, desc, reference); err != nil {
		return err
	}
	m.tags = append(m.tags, reference)
	return nil
}
