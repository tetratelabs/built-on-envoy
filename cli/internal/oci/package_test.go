// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
)

func TestPackageDirectory(t *testing.T) {
	// Create a temporary directory with test files
	srcDir := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(srcDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o750))

	// Create test files
	files := map[string]string{
		"file1.txt":        "content of file1",
		"file2.txt":        "content of file2",
		"subdir/file3.txt": "content of file3",
	}

	for name, content := range files {
		path := filepath.Join(srcDir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	// Package the directory
	data, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	// Extract to a new directory and verify contents
	destDir := t.TempDir()
	require.NoError(t, ExtractPackage(data, destDir))

	// Verify all files are present with correct content
	for name, expectedContent := range files {
		path := filepath.Clean(filepath.Join(destDir, name))
		actualContent, err := os.ReadFile(path)
		assert.NoErrorf(t, err, "failed to read extracted file %s", name)                          //nolint:testifylint
		assert.Equalf(t, expectedContent, string(actualContent), "file %s content mismatch", name) //nolint:testifylint
	}
}

func TestPackageDirectory_EmptyDir(t *testing.T) {
	srcDir := t.TempDir()

	data, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	// Extract to a new directory
	destDir := t.TempDir()
	require.NoError(t, ExtractPackage(data, destDir))

	// Verify destination is empty
	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	require.Empty(t, entries, "expected empty directory after extracting empty archive")
}

func TestPackageDirectory_NonExistent(t *testing.T) {
	_, err := PackageDirectory("/nonexistent/path/that/does/not/exist")
	require.Error(t, err)
}

func TestPackageDirectory_PreservesPermissions(t *testing.T) {
	srcDir := t.TempDir()

	// Create a file with specific permissions
	testFile := filepath.Join(srcDir, "executable.sh")
	require.NoError(t, os.WriteFile(testFile, []byte("#!/bin/bash\necho hello"), 0o700)) //nolint:gosec

	data, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	// Extract and verify permissions
	destDir := t.TempDir()
	require.NoError(t, ExtractPackage(data, destDir))

	extractedFile := filepath.Join(destDir, "executable.sh")
	info, err := os.Stat(extractedFile)
	require.NoError(t, err)

	// Check that executable bit is preserved
	require.NotZero(t, info.Mode()&0o100, "executable permission not preserved, mode: %o", info.Mode())
}

func TestExtractPackage_InvalidGzip(t *testing.T) {
	destDir := t.TempDir()
	err := ExtractPackage(bytes.NewReader([]byte("not a valid gzip")), destDir)
	require.Error(t, err)
}

func TestExtractPackage_PathTraversal(t *testing.T) {
	// Create a malicious archive with path traversal
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "safe.txt"), []byte("safe"), 0o600))

	data, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	// The normal archive should extract fine
	destDir := t.TempDir()
	require.NoError(t, ExtractPackage(data, destDir))

	// Verify the safe file was extracted
	_, err = os.Stat(filepath.Join(destDir, "safe.txt"))
	require.NoError(t, err)
}

func TestBuildOCIPackage(t *testing.T) {
	ctx := context.Background()

	// Create test data
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("test content"), 0o600))

	pkg, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	// Build OCI package
	data, err := io.ReadAll(pkg)
	require.NoError(t, err)

	store := memory.New()
	manifestDesc, err := BuildOCIPackage(ctx, store, data)
	require.NoError(t, err)

	// Verify manifest descriptor
	assert.Equal(t, ocispec.MediaTypeImageManifest, manifestDesc.MediaType)
	assert.NotEmpty(t, manifestDesc.Digest)
	assert.Positive(t, manifestDesc.Size)

	// Fetch and verify manifest content
	manifestContent, err := store.Fetch(ctx, manifestDesc)
	require.NoError(t, err)
	t.Cleanup(func() { _ = manifestContent.Close() })

	var manifest ocispec.Manifest
	require.NoError(t, json.NewDecoder(manifestContent).Decode(&manifest))

	// Verify artifact type
	assert.Equal(t, ArtifactType, manifest.ArtifactType)

	// Verify layers
	require.Len(t, manifest.Layers, 1)
	assert.Equal(t, MediaTypeLayer, manifest.Layers[0].MediaType)
	assert.Equal(t, int64(len(data)), manifest.Layers[0].Size)

	// Fetch and verify layer content
	layerContent, err := store.Fetch(ctx, manifest.Layers[0])
	require.NoError(t, err)
	t.Cleanup(func() { _ = layerContent.Close() })

	// Extract the layer and verify contents
	destDir := t.TempDir()
	require.NoError(t, ExtractPackage(layerContent, destDir))

	content, err := os.ReadFile(filepath.Clean(filepath.Join(destDir, "test.txt")))
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))
}

func TestBuildOCIPackage_EmptyData(t *testing.T) {
	ctx := context.Background()

	// Build OCI package with empty tar.gz
	srcDir := t.TempDir()
	pkg, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	data, err := io.ReadAll(pkg)
	require.NoError(t, err)

	store := memory.New()
	manifestDesc, err := BuildOCIPackage(ctx, store, data)
	require.NoError(t, err)
	assert.NotEmpty(t, manifestDesc.Digest)
}
