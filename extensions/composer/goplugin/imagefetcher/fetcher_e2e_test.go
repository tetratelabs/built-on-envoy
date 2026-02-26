// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build e2e

package imagefetcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestFetchPluginFromRealImage fetches a real plugin image from a registry
// and verifies that FetchPlugin can download and extract the plugin binary.
// This exercises the MediaType checks in fetchPluginBinary and extractImageLayer
// against real images produced by the Makefile.plugin build pipeline.
//
// The image must already be pushed to the registry before running this test.
// For local testing with a plain HTTP registry at localhost:5000:
//
//	make -f Makefile.plugin push_image EXTENSION_PATH=example \
//	  OCI_REGISTRY=localhost:5000 BOE_REGISTRY_INSECURE=true
//	TEST_BOE_REGISTRY=localhost:5000/built-on-envoy TEST_BOE_REGISTRY_INSECURE=true \
//	  go test -tags e2e -v -timeout 1m ./goplugin/imagefetcher/
//
// TEST_BOE_REGISTRY is the repository prefix (e.g. "ghcr.io/tetratelabs/built-on-envoy"
// or "localhost:5000/built-on-envoy"). The test appends "/extension-example-go:{version}".
//
// Required environment variables:
//   - TEST_BOE_REGISTRY: repository prefix for extension images
//   - TEST_BOE_REGISTRY_INSECURE: set to "true" for HTTP registries (optional)
func TestFetchPluginFromRealImage(t *testing.T) {
	registry := os.Getenv("TEST_BOE_REGISTRY")
	if registry == "" {
		t.Skip("TEST_BOE_REGISTRY not set, skipping e2e test")
	}
	insecure := os.Getenv("TEST_BOE_REGISTRY_INSECURE") == "true"

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Minute)
	t.Cleanup(cancel)

	// Use a known image reference that should exist in the registry.
	// We cannot use the version from the manifest.yaml since it may not be pushed yet,
	// so we hardcode the version here.
	ref := fmt.Sprintf("%s/extension-example-go:%s", registry, "0.2.2")
	cacheDir := t.TempDir()
	opt := Option{
		CacheDir: cacheDir,
		Insecure: insecure,
		Platform: "linux/" + runtime.GOARCH, // Plugin images are Linux-only.
	}

	path, err := FetchPlugin(ctx, ref, "example-go", opt)
	require.NoError(t, err, "FetchPlugin should succeed for real image")
	require.NotEmpty(t, path)

	// Verify the fetched file exists and is non-trivial.
	info, err := os.Stat(path)
	require.NoError(t, err, "fetched plugin file should exist")
	require.Greater(t, info.Size(), int64(1024), "fetched plugin should be larger than 1KB (real .so binary)")

	// Verify cache structure: {cacheDir}/example-go/{digest}.so
	require.Equal(t, "example-go", filepath.Base(filepath.Dir(path)))
	require.Equal(t, ".so", filepath.Ext(path))

	// Second fetch should return the cached path.
	path2, err := FetchPlugin(ctx, ref, "example-go", opt)
	require.NoError(t, err)
	require.Equal(t, path, path2, "second fetch should return cached path")
}
