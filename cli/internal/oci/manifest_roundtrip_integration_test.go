// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package oci

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

type nativeHTTPFiltersManifest struct {
	Name              string `yaml:"name"`
	NativeHTTPFilters struct {
		Before []map[string]any `yaml:"before"`
	} `yaml:"nativeHttpFilters"`
}

func TestManifestYAMLRoundTrip(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	repoRef := fmt.Sprintf("%s/test/roundtrip-native-filters", registryAddr)
	repo, err := NewRemoteRepository(logger, repoRef, &ClientOptions{PlainHTTP: true})
	require.NoError(t, err)
	client := NewRepositoryClient(logger, repo)

	srcDir := t.TempDir()
	manifestYAML := `name: test-roundtrip
minEnvoyVersion: 1.38.0
categories:
  - AI
author: test
description: Round-trip test extension
longDescription: Tests that nativeHttpFilters.before survives OCI push/pull.
type: go
tags:
  - test
license: Apache-2.0
nativeHttpFilters:
  before:
    - name: envoy.filters.http.mcp
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
        traffic_mode: REJECT_NO_MCP
        clear_route_cache: false
        request_storage_mode: DYNAMIC_METADATA_AND_FILTER_STATE
        parser_config:
          group_metadata_key: mcp_method_group
examples:
  - title: test
    description: test
    code: boe run --local .
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "manifest.yaml"), []byte(manifestYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "plugin.so"), []byte("fake"), 0o600))

	var expected nativeHTTPFiltersManifest
	require.NoError(t, yaml.Unmarshal([]byte(manifestYAML), &expected))

	tag := "v0.0.1"
	annotations := map[string]string{
		ocispec.AnnotationTitle:   "test-roundtrip",
		ocispec.AnnotationVersion: tag,
	}
	_, err = client.Push(t.Context(), srcDir, tag, annotations)
	require.NoError(t, err)

	destDir := t.TempDir()
	_, _, err = client.Pull(t.Context(), tag, destDir, nil)
	require.NoError(t, err)

	pulledData, err := os.ReadFile(filepath.Join(destDir, "manifest.yaml")) //nolint:gosec // test code, path is controlled
	require.NoError(t, err)

	var actual nativeHTTPFiltersManifest
	require.NoError(t, yaml.Unmarshal(pulledData, &actual))

	require.Equal(t, expected, actual)
}
