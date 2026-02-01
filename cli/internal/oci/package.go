// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package oci provides utilities for packaging and extracting extensions in
// OCI (Open Container Initiative) packages.
package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
)

// PackageDirectory creates a .tar.gz archive from the given directory
// and returns the compressed bytes.
func PackageDirectory(dir string) (io.Reader, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Use forward slashes for tar paths and set the relative path
		header.Name = filepath.ToSlash(relPath)

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			var link string
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}

			// Resolve the symlink target relative to the symlink's directory
			linkDir := filepath.Dir(path)
			resolvedTarget := filepath.Join(linkDir, link)
			if filepath.IsAbs(link) {
				resolvedTarget = link
			}

			// Clean and verify the resolved path is inside the package directory
			resolvedTarget = filepath.Clean(resolvedTarget)
			if !isInsideDir(resolvedTarget, dir) {
				return &ExternalSymlinkError{Path: relPath, Target: link}
			}

			header.Linkname = link
		}

		if err = tw.WriteHeader(header); err != nil {
			return err
		}

		// Only write content for regular files
		if !info.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(filepath.Clean(path))
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()

		_, err = io.Copy(tw, file)
		return err
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

const (
	maxCompressed   = 200 << 20 // 200 MB compressed
	maxUncompressed = 500 << 20 // 500 MB expanded
)

// ExtractPackage extracts a .tar.gz archive to the specified destination directory.
func ExtractPackage(data io.Reader, dest string) error {
	gr, err := gzip.NewReader(io.LimitReader(data, maxCompressed))
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(io.LimitReader(gr, maxUncompressed))

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dest, filepath.FromSlash(header.Name))

		// Protect against zip slip vulnerability
		if !isInsideDir(target, dest) {
			return &PathTraversalError{Path: header.Name}
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil { //nolint:gosec
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)) //nolint:gosec
			if err != nil {
				return err
			}
			// Suppress warning: Potential DoS vulnerability via decompression bomb
			// This is already addressed by limiting the gzip and tar readers.
			if _, err := io.Copy(file, tr); err != nil { //nolint:gosec
				_ = file.Close()
				return err
			}
			_ = file.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}

	return nil
}

// isInsideDir checks if the target path is inside the destination directory.
func isInsideDir(target, dest string) bool {
	rel, err := filepath.Rel(dest, target)
	if err != nil {
		return false
	}
	return !filepath.IsAbs(rel) && len(rel) >= 1 && rel[0] != '.'
}

// PathTraversalError is returned when an archive contains a path that would
// escape the destination directory.
type PathTraversalError struct {
	Path string
}

func (e *PathTraversalError) Error() string {
	return "path traversal attempt: " + e.Path
}

// ExternalSymlinkError is returned when a symlink points to a target outside
// the directory being packaged.
type ExternalSymlinkError struct {
	Path   string
	Target string
}

func (e *ExternalSymlinkError) Error() string {
	return "symlink " + e.Path + " points outside package directory: " + e.Target
}

const (
	// MediaTypeLayer is the media type for extension layer content.
	MediaTypeLayer = "application/vnd.oci.image.layer.v1.tar+gzip"
	// ArtifactType is the artifact type for extension packages.
	ArtifactType = "application/vnd.builtonenvoy.extension.v1"
)

// BuildOCIPackage creates an OCI artifact from a .tar.gz byte array and stores it
// in the provided store. The artifact contains a single layer with the provided content.
// Returns the manifest descriptor.
func BuildOCIPackage(ctx context.Context, store content.Pusher, data []byte, annotations map[string]string) (ocispec.Descriptor, error) {
	// Push the layer content
	layerDesc, err := oras.PushBytes(ctx, store, MediaTypeLayer, data)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[ocispec.AnnotationCreated] = time.Now().UTC().Format(time.RFC3339)

	// Pack the manifest with the layer
	packOpts := oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layerDesc},
		ManifestAnnotations: annotations,
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, ArtifactType, packOpts)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return manifestDesc, nil
}
