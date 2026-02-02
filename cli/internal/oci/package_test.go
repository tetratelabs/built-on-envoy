// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

func TestPackageDirectory_Symlinks(t *testing.T) {
	srcDir := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(srcDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o750))

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "original.txt"), []byte("original content"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested content"), 0o600))

	// Create symlinks:
	// 1. Symlink to a file in the same directory
	require.NoError(t, os.Symlink("original.txt", filepath.Join(srcDir, "link_to_original.txt")))
	// 2. Symlink to a file in a subdirectory
	require.NoError(t, os.Symlink("subdir/nested.txt", filepath.Join(srcDir, "link_to_nested.txt")))
	// 3. Symlink inside a subdirectory pointing to parent
	require.NoError(t, os.Symlink("../original.txt", filepath.Join(subDir, "link_to_parent.txt")))

	// Package the directory
	data, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	// Extract to a new directory
	destDir := t.TempDir()
	require.NoError(t, ExtractPackage(data, destDir))

	// Verify symlinks are preserved as symlinks (not copied as files)
	tests := []struct {
		linkPath       string
		expectedTarget string
	}{
		{"link_to_original.txt", "original.txt"},
		{"link_to_nested.txt", "subdir/nested.txt"},
		{"subdir/link_to_parent.txt", "../original.txt"},
	}

	for _, tc := range tests {
		linkPath := filepath.Join(destDir, tc.linkPath)

		// Verify it's a symlink
		var info os.FileInfo
		info, err = os.Lstat(linkPath)
		require.NoErrorf(t, err, "failed to stat symlink %s", tc.linkPath)
		require.NotZero(t, info.Mode()&os.ModeSymlink, "expected %s to be a symlink", tc.linkPath)

		// Verify the symlink target
		var target string
		target, err = os.Readlink(linkPath)
		require.NoErrorf(t, err, "failed to read symlink %s", tc.linkPath)
		require.Equalf(t, tc.expectedTarget, target, "symlink %s has wrong target", tc.linkPath)
	}

	// Verify symlinks resolve to correct content
	content, err := os.ReadFile(filepath.Clean(filepath.Join(destDir, "link_to_original.txt")))
	require.NoError(t, err)
	assert.Equal(t, "original content", string(content))

	content, err = os.ReadFile(filepath.Clean(filepath.Join(destDir, "link_to_nested.txt")))
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(content))

	content, err = os.ReadFile(filepath.Clean(filepath.Join(destDir, "subdir", "link_to_parent.txt")))
	require.NoError(t, err)
	assert.Equal(t, "original content", string(content))
}

func TestPackageDirectory_SymlinkToExternalFile(t *testing.T) {
	// Create a file outside the directory being packaged
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "external.txt")
	require.NoError(t, os.WriteFile(externalFile, []byte("external content"), 0o600))

	// Create the directory to package with a symlink pointing outside
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "internal.txt"), []byte("internal content"), 0o600))

	// Create a symlink to the external file (using absolute path)
	require.NoError(t, os.Symlink(externalFile, filepath.Join(srcDir, "link_to_external.txt")))

	// Package should fail because the symlink points outside the directory
	_, err := PackageDirectory(srcDir)
	require.Error(t, err)

	var symlinkErr *ExternalSymlinkError
	require.ErrorAs(t, err, &symlinkErr)
	require.Equal(t, "link_to_external.txt", symlinkErr.Path)
	require.Equal(t, externalFile, symlinkErr.Target)
	require.ErrorContains(t, err, symlinkErr.Path)
	require.ErrorContains(t, err, symlinkErr.Target)
}

func TestPackageDirectory_SymlinkEscapesWithRelativePath(t *testing.T) {
	// Create a parent directory with a file
	parentDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "secret.txt"), []byte("secret"), 0o600))

	// Create a subdirectory to package
	srcDir := filepath.Join(parentDir, "package")
	require.NoError(t, os.Mkdir(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "internal.txt"), []byte("internal"), 0o600))

	// Create a symlink that escapes using relative path
	require.NoError(t, os.Symlink("../secret.txt", filepath.Join(srcDir, "escape_link.txt")))

	// Package should fail because the symlink escapes the directory
	_, err := PackageDirectory(srcDir)
	require.Error(t, err)

	var symlinkErr *ExternalSymlinkError
	require.ErrorAs(t, err, &symlinkErr)
	require.Equal(t, "escape_link.txt", symlinkErr.Path)
	require.Equal(t, "../secret.txt", symlinkErr.Target)
	require.ErrorContains(t, err, symlinkErr.Path)
	require.ErrorContains(t, err, symlinkErr.Target)
}

func TestExtractPackage_InvalidGzip(t *testing.T) {
	destDir := t.TempDir()
	err := ExtractPackage(bytes.NewReader([]byte("not a valid gzip")), destDir)
	require.Error(t, err)
}

func TestExtractPackage_PathTraversal(t *testing.T) {
	// Create a malicious tar.gz archive with path traversal attempt
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add a file with a path traversal attempt
	maliciousPath := "../../../tmp/malicious.txt"
	content := []byte("malicious content")
	header := &tar.Header{
		Name: maliciousPath,
		Mode: 0o600,
		Size: int64(len(content)),
	}
	require.NoError(t, tw.WriteHeader(header))
	_, err := tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	// Attempt to extract the malicious archive
	destDir := t.TempDir()
	err = ExtractPackage(&buf, destDir)

	// Should fail with PathTraversalError
	require.Error(t, err)
	var pathErr *PathTraversalError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, maliciousPath, pathErr.Path)
	assert.ErrorContains(t, err, maliciousPath)
}

func TestExtractInvalidLocation(t *testing.T) {
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "safe.txt"), []byte("safe"), 0o600))

	data, err := PackageDirectory(srcDir)
	require.NoError(t, err)

	err = ExtractPackage(data, "/dev/null")
	require.Error(t, err)
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
	annotations := map[string]string{
		ocispec.AnnotationTitle:       "test-extension",
		ocispec.AnnotationVersion:     "1.0.0",
		ocispec.AnnotationDescription: "A test extension",
	}
	manifestDesc, err := BuildOCIPackage(ctx, store, data, annotations)
	require.NoError(t, err)

	// Verify manifest descriptor
	assert.Equal(t, ocispec.MediaTypeImageManifest, manifestDesc.MediaType)
	assert.NotEmpty(t, manifestDesc.Digest)
	assert.Positive(t, manifestDesc.Size)
	assert.Equal(t, "test-extension", manifestDesc.Annotations[ocispec.AnnotationTitle])
	assert.Equal(t, "1.0.0", manifestDesc.Annotations[ocispec.AnnotationVersion])
	assert.Equal(t, "A test extension", manifestDesc.Annotations[ocispec.AnnotationDescription])
	assert.NotEmpty(t, manifestDesc.Annotations[ocispec.AnnotationCreated])

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
	manifestDesc, err := BuildOCIPackage(ctx, store, data, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, manifestDesc.Digest)
	require.NotEmpty(t, manifestDesc.Annotations[ocispec.AnnotationCreated])
}
