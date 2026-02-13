// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// getCachePath returns the platform-specific cache directory for plugins.
func getCachePath() (string, error) {
	var baseDir string
	switch runtime.GOOS {
	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, "Library", "Caches", "built-on-envoy", "plugins")
	case "linux":
		// Check XDG_CACHE_HOME first, fallback to ~/.cache
		if cacheDir := os.Getenv("XDG_CACHE_HOME"); cacheDir != "" {
			baseDir = filepath.Join(cacheDir, "built-on-envoy", "plugins")
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get user home directory: %w", err)
			}
			baseDir = filepath.Join(homeDir, ".cache", "built-on-envoy", "plugins")
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	return baseDir, nil
}

// generateCacheKey creates a unique cache key for the given image reference and platform.
func generateCacheKey(imageRef, platform, arch string) string {
	// Create a hash-based cache key to handle special characters in image refs
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", imageRef, platform, arch)))
	return fmt.Sprintf("%x.so", hash[:16])
}

// getCachedPluginPath checks if a plugin exists in cache and returns its path.
func getCachedPluginPath(imageRef, platform, arch string) (string, error) {
	cacheDir, err := getCachePath()
	if err != nil {
		return "", err
	}

	cacheKey := generateCacheKey(imageRef, platform, arch)
	cachedPath := filepath.Join(cacheDir, cacheKey)

	// Check if file exists
	if _, err := os.Stat(cachedPath); err == nil {
		return cachedPath, nil
	}

	return "", nil
}

// saveCachedPlugin saves the plugin binary to cache and returns the cache path.
func saveCachedPlugin(imageRef, platform, arch string, data []byte) (string, error) {
	cacheDir, err := getCachePath()
	if err != nil {
		return "", err
	}

	cacheKey := generateCacheKey(imageRef, platform, arch)
	cachedPath := filepath.Join(cacheDir, cacheKey)

	// Write to a temporary file first, then rename for atomicity
	tmpPath := cachedPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o700); err != nil {
		return "", fmt.Errorf("failed to write cached plugin: %w", err)
	}

	if err := os.Rename(tmpPath, cachedPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to finalize cached plugin: %w", err)
	}

	return cachedPath, nil
}
