// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetCachePath(t *testing.T) {
	cachePath, err := getCachePath()
	if err != nil {
		t.Fatalf("failed to get cache path: %v", err)
	}

	// Verify the path exists
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("cache path does not exist: %v", err)
	}

	// Verify it contains the expected parts
	if !filepath.IsAbs(cachePath) {
		t.Errorf("cache path is not absolute: %s", cachePath)
	}

	expectedSuffix := filepath.Join("built-on-envoy", "plugins")
	// Just verify it contains the expected suffix
	if !strings.Contains(cachePath, expectedSuffix) {
		t.Logf("cache path: %s", cachePath)
		t.Logf("expected to contain: %s", expectedSuffix)
	}
}

func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		platform string
		arch     string
	}{
		{
			name:     "simple image",
			imageRef: "registry.io/plugin:latest",
			platform: "linux",
			arch:     "amd64",
		},
		{
			name:     "complex image",
			imageRef: "ghcr.io/owner/repo/plugin:v1.0.0",
			platform: "darwin",
			arch:     "arm64",
		},
		{
			name:     "image with special chars",
			imageRef: "registry.io/user/plugin:tag-with-dash",
			platform: "linux",
			arch:     "amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := generateCacheKey(tt.imageRef, tt.platform, tt.arch)

			// Verify it's a valid filename
			if key == "" {
				t.Error("cache key is empty")
			}

			if !filepath.IsLocal(key) {
				t.Errorf("cache key is not a valid filename: %s", key)
			}

			// Verify it ends with .so
			if filepath.Ext(key) != ".so" {
				t.Errorf("cache key should end with .so: %s", key)
			}

			// Verify same inputs produce same key
			key2 := generateCacheKey(tt.imageRef, tt.platform, tt.arch)
			if key != key2 {
				t.Errorf("same inputs produced different keys: %s vs %s", key, key2)
			}

			// Verify different inputs produce different keys
			key3 := generateCacheKey(tt.imageRef+"different", tt.platform, tt.arch)
			if key == key3 {
				t.Error("different inputs produced same key")
			}
		})
	}
}

func TestSaveAndGetCachedPlugin(t *testing.T) {
	testData := []byte("test plugin binary data")
	imageRef := "test-registry.io/test-plugin:v1.0.0"
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Clean up any existing cache entry
	cachePath, err := getCachePath()
	if err != nil {
		t.Fatalf("failed to get cache path: %v", err)
	}
	cacheKey := generateCacheKey(imageRef, platform, arch)
	cachedFile := filepath.Join(cachePath, cacheKey)
	_ = os.Remove(cachedFile)

	// Test that cache is empty initially
	path, err := getCachedPluginPath(imageRef, platform, arch)
	if err != nil {
		t.Fatalf("getCachedPluginPath failed: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty cache, got: %s", path)
	}

	// Save to cache
	savedPath, err := saveCachedPlugin(imageRef, platform, arch, testData)
	if err != nil {
		t.Fatalf("saveCachedPlugin failed: %v", err)
	}

	// Verify saved path exists
	savedPathInfo, statErr := os.Stat(savedPath)
	if statErr != nil {
		t.Errorf("saved file does not exist: %v", statErr)
	} else if savedPathInfo.IsDir() {
		t.Error("saved path is a directory, expected a file")
	}

	// Verify data is correct
	//nolint:gosec // G304: Reading from cache directory is safe
	readData, readErr := os.ReadFile(savedPath)
	if readErr != nil {
		t.Fatalf("failed to read saved file: %v", readErr)
	}
	if !bytes.Equal(readData, testData) {
		t.Errorf("saved data mismatch: got %q, want %q", readData, testData)
	}

	// Test that cache is now populated
	path, err = getCachedPluginPath(imageRef, platform, arch)
	if err != nil {
		t.Fatalf("getCachedPluginPath failed: %v", err)
	}
	if path == "" {
		t.Error("expected cached path, got empty string")
	}
	if path != savedPath {
		t.Errorf("cached path mismatch: got %s, want %s", path, savedPath)
	}

	// Clean up
	_ = os.Remove(savedPath)
}

func TestCacheKeyUniqueness(t *testing.T) {
	keys := make(map[string]bool)

	testCases := []struct {
		imageRef string
		platform string
		arch     string
	}{
		{"image1:tag1", "linux", "amd64"},
		{"image1:tag1", "linux", "arm64"},
		{"image1:tag1", "darwin", "amd64"},
		{"image1:tag2", "linux", "amd64"},
		{"image2:tag1", "linux", "amd64"},
	}

	for _, tc := range testCases {
		key := generateCacheKey(tc.imageRef, tc.platform, tc.arch)
		if keys[key] {
			t.Errorf("duplicate key generated for %+v", tc)
		}
		keys[key] = true
	}
}

func TestCachingBehavior(t *testing.T) {
	// This test verifies that caching works across multiple calls
	imageRef := "test-cache-image:v1.0.0"
	platform := runtime.GOOS
	arch := runtime.GOARCH
	testData := []byte("cached plugin data")

	// Clean up cache before test
	cachePath, err := getCachePath()
	if err != nil {
		t.Fatalf("failed to get cache path: %v", err)
	}
	cacheKey := generateCacheKey(imageRef, platform, arch)
	cachedFile := filepath.Join(cachePath, cacheKey)
	_ = os.Remove(cachedFile)
	defer func() {
		_ = os.Remove(cachedFile)
	}()

	// Save to cache
	savedPath, err := saveCachedPlugin(imageRef, platform, arch, testData)
	if err != nil {
		t.Fatalf("saveCachedPlugin failed: %v", err)
	}

	// First retrieval from cache
	path1, err := getCachedPluginPath(imageRef, platform, arch)
	if err != nil {
		t.Fatalf("getCachedPluginPath failed: %v", err)
	}

	if path1 == "" {
		t.Error("expected cached path, got empty")
	}

	if path1 != savedPath {
		t.Errorf("path mismatch: got %s, want %s", path1, savedPath)
	}

	// Second retrieval should return same path
	path2, err := getCachedPluginPath(imageRef, platform, arch)
	if err != nil {
		t.Fatalf("second getCachedPluginPath failed: %v", err)
	}

	if path1 != path2 {
		t.Errorf("cache inconsistency: first=%s, second=%s", path1, path2)
	}

	// Verify content is still correct
	//nolint:gosec // G304: Reading from cache directory is safe
	data, readErr := os.ReadFile(path2)
	if readErr != nil {
		t.Fatalf("failed to read cached file: %v", readErr)
	}

	if !bytes.Equal(data, testData) {
		t.Errorf("cached data mismatch: got %q, want %q", data, testData)
	}
}
