// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
)

type clientTest struct {
	name      string
	newClient func(t *testing.T) Client
}

var clientTests = []clientTest{
	{
		name:      "in-memory store",
		newClient: func(*testing.T) Client { return NewClient(memory.New()) },
	},
}

func TestClient_PushPull(t *testing.T) {
	for _, tt := range clientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newClient(t)

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
			digest, err := client.Push(ctx, srcDir, tag)
			require.NoError(t, err)
			assert.NotEmpty(t, digest)
			assert.Contains(t, digest, "sha256:")

			// Pull to a new directory
			destDir := t.TempDir()
			pulledDigest, err := client.Pull(ctx, tag, destDir)
			require.NoError(t, err)
			assert.Equal(t, digest, pulledDigest)

			// Verify all files were extracted correctly
			for name, expectedContent := range files {
				content, err := os.ReadFile(filepath.Clean(filepath.Join(destDir, name)))
				require.NoError(t, err, "failed to read file %s", name)
				assert.Equal(t, expectedContent, string(content), "content mismatch for %s", name)
			}
		})
	}
}

func TestClient_PushPull_MultipleTags(t *testing.T) {
	for _, tt := range clientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newClient(t)

			// Create and push first version
			srcDir1 := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(srcDir1, "version.txt"), []byte("v1"), 0o600))

			digest1, err := client.Push(ctx, srcDir1, "v1")
			require.NoError(t, err)

			// Create and push second version
			srcDir2 := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(srcDir2, "version.txt"), []byte("v2"), 0o600))

			digest2, err := client.Push(ctx, srcDir2, "v2")
			require.NoError(t, err)

			// Digests should be different
			assert.NotEqual(t, digest1, digest2)

			// Pull v1 and verify
			destDir1 := t.TempDir()
			_, err = client.Pull(ctx, "v1", destDir1)
			require.NoError(t, err)

			content1, err := os.ReadFile(filepath.Clean(filepath.Join(destDir1, "version.txt")))
			require.NoError(t, err)
			assert.Equal(t, "v1", string(content1))

			// Pull v2 and verify
			destDir2 := t.TempDir()
			_, err = client.Pull(ctx, "v2", destDir2)
			require.NoError(t, err)

			content2, err := os.ReadFile(filepath.Clean(filepath.Join(destDir2, "version.txt")))
			require.NoError(t, err)
			assert.Equal(t, "v2", string(content2))
		})
	}
}

func TestClient_Pull_NonExistentTag(t *testing.T) {
	for _, tt := range clientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newClient(t)

			destDir := t.TempDir()
			_, err := client.Pull(ctx, "nonexistent", destDir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to pull from registry")
		})
	}
}

func TestClient_Push_NonExistentPath(t *testing.T) {
	for _, tt := range clientTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			client := tt.newClient(t)

			_, err := client.Push(ctx, "/nonexistent/path", "v1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to package directory")
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
	opts := &RepositoryOptions{
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
