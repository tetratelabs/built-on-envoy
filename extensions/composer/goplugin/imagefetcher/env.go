// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package imagefetcher provides OCI image fetching for Go plugin binaries.
package imagefetcher

import (
	"fmt"
	"os"
	"path/filepath"
)

// OptionFromEnv reads fetcher configuration from environment variables:
//   - GOPLUGIN_CACHE_DIR   — cache root (default: os.TempDir()/goplugin-cache)
//   - GOPLUGIN_PULL_SECRET — path to Docker config JSON file
//   - GOPLUGIN_INSECURE    — "true" to allow insecure registries
func OptionFromEnv() Option {
	opt := Option{}

	if dir := os.Getenv("GOPLUGIN_CACHE_DIR"); dir != "" {
		opt.CacheDir = dir
	} else {
		opt.CacheDir = filepath.Join(os.TempDir(), "goplugin-cache")
	}

	if secretPath := os.Getenv("GOPLUGIN_PULL_SECRET"); secretPath != "" {
		data, err := os.ReadFile(secretPath) //nolint:gosec // Path comes from trusted env var.
		if err != nil {
			fmt.Printf("warning: failed to read GOPLUGIN_PULL_SECRET %s: %v\n", secretPath, err)
		} else {
			opt.PullSecret = data
		}
	}

	if os.Getenv("GOPLUGIN_INSECURE") == "true" {
		opt.Insecure = true
	}

	return opt
}
