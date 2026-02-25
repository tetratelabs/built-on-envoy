// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package imagefetcher provides OCI image fetching for Go plugin binaries.
package imagefetcher

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
)

// OptionFromEnv reads fetcher configuration from environment variables.
//
// Cache directory precedence:
//
//	GOPLUGIN_CACHE_DIR > $BOE_DATA_HOME/goplugin-cache > os.TempDir()/goplugin-cache
//
// Insecure registry precedence:
//
//	GOPLUGIN_INSECURE > BOE_REGISTRY_INSECURE > false
//
// Pull secret:
//
//	GOPLUGIN_PULL_SECRET — path to Docker config JSON file
func OptionFromEnv() Option {
	opt := Option{}

	opt.CacheDir = os.Getenv("GOPLUGIN_CACHE_DIR")
	if opt.CacheDir == "" {
		baseDir := cmp.Or(
			os.Getenv("BOE_DATA_HOME"),
			os.TempDir(),
		)
		opt.CacheDir = filepath.Join(baseDir, "goplugin-cache")
	}

	if secretPath := os.Getenv("GOPLUGIN_PULL_SECRET"); secretPath != "" {
		data, err := os.ReadFile(filepath.Clean(secretPath))
		if err != nil {
			fmt.Printf("warning: failed to read GOPLUGIN_PULL_SECRET %s: %v\n", secretPath, err)
		} else {
			opt.PullSecret = data
		}
	}

	opt.Insecure = cmp.Or(
		os.Getenv("GOPLUGIN_INSECURE"),
		os.Getenv("BOE_REGISTRY_INSECURE"),
	) == "true"

	return opt
}
