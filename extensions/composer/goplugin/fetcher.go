// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// fetchImageFromRegistry downloads a plugin from an OCI registry.
func fetchImageFromRegistry(imageRef string) ([]byte, error) {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Check cache first
	cachedPath, err := getCachedPluginPath(imageRef, platform, arch)
	if err != nil {
		return nil, fmt.Errorf("failed to check cache: %w", err)
	}
	if cachedPath != "" {
		// Read from cache
		//nolint:gosec // G304: Reading from cache directory is safe
		data, readErr := os.ReadFile(cachedPath)
		if readErr == nil {
			return data, nil
		}
		// If cache read fails, continue to download
	}

	// Parse image reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Create platform-specific descriptor
	targetPlatform := v1.Platform{
		OS:           platform,
		Architecture: arch,
	}

	// Setup remote options with platform selection
	// Note: insecure registries are allowed by default
	options := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithPlatform(targetPlatform),
		remote.WithTransport(remote.DefaultTransport),
	}

	// Fetch image descriptor
	descriptor, err := remote.Get(ref, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	// Check if this is an index (multi-platform manifest)
	var img v1.Image
	if descriptor.MediaType.IsIndex() {
		// This is a multi-platform image, get the platform-specific image
		imageIndex, indexErr := descriptor.ImageIndex()
		if indexErr != nil {
			return nil, fmt.Errorf("failed to get image index: %w", indexErr)
		}

		// Get the manifest for our platform
		manifest, manifestErr := imageIndex.IndexManifest()
		if manifestErr != nil {
			return nil, fmt.Errorf("failed to get index manifest: %w", manifestErr)
		}

		// Find the matching platform
		var platformHash v1.Hash
		for i := range manifest.Manifests {
			desc := &manifest.Manifests[i]
			if desc.Platform != nil &&
				desc.Platform.OS == platform &&
				desc.Platform.Architecture == arch {
				platformHash = desc.Digest
				break
			}
		}

		if platformHash.Algorithm == "" {
			return nil, fmt.Errorf("no image found for platform %s/%s", platform, arch)
		}

		// Get the platform-specific image
		img, err = imageIndex.Image(platformHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get platform-specific image: %w", err)
		}
	} else {
		// Single platform image
		img, err = descriptor.Image()
		if err != nil {
			return nil, fmt.Errorf("failed to get image: %w", err)
		}
	}

	// Extract plugin binary from image
	pluginData, err := extractPluginFromImage(img, platform, arch)
	if err != nil {
		return nil, fmt.Errorf("failed to extract plugin from image: %w", err)
	}

	// Save to cache
	if _, err := saveCachedPlugin(imageRef, platform, arch, pluginData); err != nil {
		// Log warning but don't fail if caching fails
		fmt.Printf("warning: failed to cache plugin: %v\n", err)
	}

	return pluginData, nil
}

// extractPluginFromImage extracts the plugin binary from a container image.
func extractPluginFromImage(img v1.Image, _, _ string) ([]byte, error) {
	// Get image layers
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to get image layers: %w", err)
	}

	// Look for the plugin binary in the layers
	// Common locations: /plugin.so, /app/plugin.so, /usr/local/bin/*.so
	possiblePaths := []string{
		"plugin.so",
		"app/plugin.so",
		"usr/local/bin/plugin.so",
	}

	for _, layer := range layers {
		uncompressed, err := layer.Uncompressed()
		if err != nil {
			continue
		}

		tr := tar.NewReader(uncompressed)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			// Skip directories
			if header.Typeflag == tar.TypeDir {
				continue
			}

			// Check if this is a .so file
			if !strings.HasSuffix(header.Name, ".so") {
				continue
			}

			// Check if it matches expected paths or is the only .so file
			matched := false
			for _, path := range possiblePaths {
				if strings.HasSuffix(header.Name, path) {
					matched = true
					break
				}
			}

			// If it's a .so file and either matches expected paths or is the first .so we find
			if matched || strings.Count(header.Name, "/") <= 1 {
				// Read the file content
				var buf bytes.Buffer
				// #nosec G110 - This is expected plugin content from trusted registry
				if _, err := io.Copy(&buf, tr); err != nil {
					continue
				}
				return buf.Bytes(), nil
			}
		}
		if closeErr := uncompressed.Close(); closeErr != nil {
			// Log but don't fail on close error
			fmt.Printf("warning: failed to close layer reader: %v\n", closeErr)
		}
	}

	return nil, fmt.Errorf("no plugin binary (.so file) found in image")
}
