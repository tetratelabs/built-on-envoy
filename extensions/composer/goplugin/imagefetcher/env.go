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

	switch {
	case os.Getenv("GOPLUGIN_CACHE_DIR") != "":
		opt.CacheDir = os.Getenv("GOPLUGIN_CACHE_DIR")
	case os.Getenv("BOE_DATA_HOME") != "":
		opt.CacheDir = filepath.Join(os.Getenv("BOE_DATA_HOME"), "goplugin-cache")
	default:
		opt.CacheDir = filepath.Join(os.TempDir(), "goplugin-cache")
	}

	if secretPath := os.Getenv("GOPLUGIN_PULL_SECRET"); secretPath != "" {
		data, err := os.ReadFile(filepath.Clean(secretPath))
		if err != nil {
			fmt.Printf("warning: failed to read GOPLUGIN_PULL_SECRET %s: %v\n", secretPath, err)
		} else {
			opt.PullSecret = data
		}
	}

	if v := os.Getenv("GOPLUGIN_INSECURE"); v != "" {
		opt.Insecure = v == "true"
	} else if os.Getenv("BOE_REGISTRY_INSECURE") == "true" {
		opt.Insecure = true
	}

	return opt
}
