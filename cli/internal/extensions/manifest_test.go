// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestAllManifestsAreValid(t *testing.T) {
	_, err := LoadManifests(internaltesting.ExtensionsFS(t), ".", true)
	require.NoError(t, err)
}

func TestAllManifestsAreLoaded(t *testing.T) {
	fsys := internaltesting.ExtensionsFS(t)

	count := 0
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) == "manifest.yaml" {
			count++
		}
		return nil
	})
	require.NoError(t, err)

	manifests, err := LoadManifests(fsys, ".", false)
	require.NoError(t, err)
	require.Len(t, manifests, count)
}

func TestValidateGoManifest(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"go_empty_version.yaml", "", true},
		{"go_invalid_version.yaml", "", true},
		{"go_missing_version.yaml", "", true},
		{"go_missing_composer_version.yaml", "", true},
		{"go_valid.yaml", "1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.name)
			localManifest, err := LoadLocalManifest(manifestPath)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, localManifest.ComposerVersion)
			}
		})
	}
}

func TestValidateLuaManifest(t *testing.T) {
	wantInline := &Lua{Inline: `function envoy_on_request(request_handle)
  request_handle:logInfo("Hello, World!")
end
`}

	tests := []struct {
		name    string
		want    *Lua
		wantErr bool
	}{
		{"lua_invalid_inline_and_path.yaml", nil, true},
		{"lua_invalid_missing_settings.yaml", nil, true},
		{"lua_in_wrong_type.yaml", nil, true},
		{"lua_valid_path.yaml", &Lua{Path: "extension.lua"}, false},
		{"lua_valid_inline.yaml", wantInline, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.name)
			localManifest, err := LoadLocalManifest(manifestPath)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, localManifest.Lua)
			}
		})
	}
}

func TestValidateExtProcManifest(t *testing.T) {
	wantFull := &ExtProc{
		GRPCPort:         50051,
		FailureModeAllow: true,
		MessageTimeout:   "200ms",
		ProcessingMode: &ExtProcProcessingMode{
			RequestHeaderMode:  "SEND",
			ResponseHeaderMode: "SKIP",
			RequestBodyMode:    "BUFFERED",
			ResponseBodyMode:   "NONE",
		},
	}
	wantMinimal := &ExtProc{GRPCPort: 50051}

	tests := []struct {
		name    string
		want    *ExtProc
		wantErr bool
	}{
		{"ext_proc_valid_full.yaml", wantFull, false},
		{"ext_proc_valid_minimal.yaml", wantMinimal, false},
		{"ext_proc_missing_settings.yaml", nil, true},
		{"ext_proc_in_wrong_type.yaml", nil, true},
		{"ext_proc_invalid_header_mode.yaml", nil, true},
		{"ext_proc_invalid_body_mode.yaml", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.name)
			localManifest, err := LoadLocalManifest(manifestPath)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, localManifest.ExtProc)
			}
		})
	}
}

func TestManifestsForCatalog(t *testing.T) {
	all, err := LoadManifests(internaltesting.ExtensionsFS(t), ".", false)
	require.NoError(t, err)
	manifests := ManifestsIndex(all)
	require.Less(t, len(manifests), len(all))
	for _, m := range manifests {
		require.Falsef(t, m.ExtensionSet, "manifest %s should not be included in catalog", m.Name)
	}
}

func TestSupportsEnvoyVersion(t *testing.T) {
	tests := []struct {
		name            string
		minEnvoyVersion string
		maxEnvoyVersion string
		version         string
		want            bool
	}{
		{
			name:    "no constraints",
			version: "1.30.0",
			want:    true,
		},
		{
			name:            "min only - version equal",
			minEnvoyVersion: "1.30.0",
			version:         "1.30.0",
			want:            true,
		},
		{
			name:            "min only - version above",
			minEnvoyVersion: "1.30.0",
			version:         "1.31.0",
			want:            true,
		},
		{
			name:            "min only - version below",
			minEnvoyVersion: "1.30.0",
			version:         "1.29.0",
			want:            false,
		},
		{
			name:            "max only - version equal",
			maxEnvoyVersion: "1.30.0",
			version:         "1.30.0",
			want:            true,
		},
		{
			name:            "max only - version below",
			maxEnvoyVersion: "1.30.0",
			version:         "1.29.0",
			want:            true,
		},
		{
			name:            "max only - version above",
			maxEnvoyVersion: "1.30.0",
			version:         "1.31.0",
			want:            false,
		},
		{
			name:            "range - version within",
			minEnvoyVersion: "1.28.0",
			maxEnvoyVersion: "1.32.0",
			version:         "1.30.0",
			want:            true,
		},
		{
			name:            "range - version at min",
			minEnvoyVersion: "1.28.0",
			maxEnvoyVersion: "1.32.0",
			version:         "1.28.0",
			want:            true,
		},
		{
			name:            "range - version at max",
			minEnvoyVersion: "1.28.0",
			maxEnvoyVersion: "1.32.0",
			version:         "1.32.0",
			want:            true,
		},
		{
			name:            "range - version below min",
			minEnvoyVersion: "1.28.0",
			maxEnvoyVersion: "1.32.0",
			version:         "1.27.0",
			want:            false,
		},
		{
			name:            "range - version above max",
			minEnvoyVersion: "1.28.0",
			maxEnvoyVersion: "1.32.0",
			version:         "1.33.0",
			want:            false,
		},
		{
			name:            "patch version comparison",
			minEnvoyVersion: "1.30.1",
			version:         "1.30.0",
			want:            false,
		},
		{
			name:            "patch version comparison - above",
			minEnvoyVersion: "1.30.1",
			version:         "1.30.2",
			want:            true,
		},
		{
			name:    "dev - no constraints",
			version: "dev",
			want:    true,
		},
		{
			name:            "dev - with min",
			minEnvoyVersion: "1.38.0",
			version:         "dev",
			want:            true,
		},
		{
			name:            "dev - with max",
			maxEnvoyVersion: "1.39.0",
			version:         "dev",
			want:            true,
		},
		{
			name:            "dev - with min and max",
			minEnvoyVersion: "1.38.0",
			maxEnvoyVersion: "1.39.0",
			version:         "dev",
			want:            true,
		},
		{
			name:            "dev-latest - with min and max",
			minEnvoyVersion: "1.38.0",
			maxEnvoyVersion: "1.39.0",
			version:         "dev-latest",
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manifest{
				MinEnvoyVersion: tt.minEnvoyVersion,
				MaxEnvoyVersion: tt.maxEnvoyVersion,
			}
			got := m.SupportsEnvoyVersion(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnvoyConstraints(t *testing.T) {
	tests := []struct {
		name            string
		minEnvoyVersion string
		maxEnvoyVersion string
		want            string
	}{
		{
			name: "no constraints",
			want: "",
		},
		{
			name:            "min only",
			minEnvoyVersion: "1.30.0",
			want:            ">= 1.30.0",
		},
		{
			name:            "max only",
			maxEnvoyVersion: "1.32.0",
			want:            "<= 1.32.0",
		},
		{
			name:            "both min and max",
			minEnvoyVersion: "1.28.0",
			maxEnvoyVersion: "1.32.0",
			want:            ">= 1.28.0 && <= 1.32.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manifest{
				MinEnvoyVersion: tt.minEnvoyVersion,
				MaxEnvoyVersion: tt.maxEnvoyVersion,
			}
			got := m.EnvoyConstraints()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestImplicitMaxEnvoyVersion(t *testing.T) {
	tests := []struct {
		name       string
		minVersion string
		want       string
	}{
		{
			name:       "standard version",
			minVersion: "1.33.0",
			want:       "1.34.0",
		},
		{
			name:       "minor version zero",
			minVersion: "1.0.0",
			want:       "1.1.0",
		},
		{
			name:       "non-zero patch",
			minVersion: "1.33.5",
			want:       "1.34.0",
		},
		{
			name:       "missing patch segment",
			minVersion: "1.33",
			want:       "1.34.0",
		},
		{
			name:       "empty string",
			minVersion: "",
			want:       "",
		},
		{
			name:       "major only",
			minVersion: "1",
			want:       "",
		},
		{
			name:       "invalid minor",
			minVersion: "1.abc.0",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ImplicitMaxEnvoyVersion(tt.minVersion)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateParentManifest(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"parent_valid.yaml", false},
		{"parent_invalid_type.yaml", true},
		{"parent_missing.yaml", true},
		{"parent_with_version.yaml", true},
		{"parent_with_composer_version.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.name)
			_, err := LoadLocalManifest(manifestPath)
			require.Equal(t, tt.wantErr, err != nil, err)
		})
	}
}

func TestHighestMinEnvoyVersion(t *testing.T) {
	tests := []struct {
		name      string
		manifests []*Manifest
		want      string
	}{
		{
			name:      "empty list",
			manifests: nil,
			want:      "",
		},
		{
			name:      "no min versions set",
			manifests: []*Manifest{{}, {MaxEnvoyVersion: "1.30.0"}},
			want:      "",
		},
		{
			name: "single manifest with min",
			manifests: []*Manifest{
				{MinEnvoyVersion: "1.28.0"},
			},
			want: "1.28.0",
		},
		{
			name: "multiple manifests - picks highest",
			manifests: []*Manifest{
				{MinEnvoyVersion: "1.28.0"},
				{MinEnvoyVersion: "1.31.0"},
				{MinEnvoyVersion: "1.31.1"},
				{},
				{MinEnvoyVersion: "1.29.0"},
			},
			want: "1.31.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HighestMinEnvoyVersion(tt.manifests)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLowestMaxEnvoyVersion(t *testing.T) {
	tests := []struct {
		name      string
		manifests []*Manifest
		want      string
	}{
		{
			name:      "empty list",
			manifests: nil,
			want:      "",
		},
		{
			name:      "no max versions set",
			manifests: []*Manifest{{}, {MinEnvoyVersion: "1.28.0"}},
			want:      "",
		},
		{
			name: "single manifest with max",
			manifests: []*Manifest{
				{MaxEnvoyVersion: "1.32.0"},
			},
			want: "1.32.0",
		},
		{
			name: "multiple manifests - picks lowest",
			manifests: []*Manifest{
				{MaxEnvoyVersion: "1.35.0"},
				{MaxEnvoyVersion: "1.30.0"},
				{MaxEnvoyVersion: "1.30.1"},
				{},
				{MaxEnvoyVersion: "1.33.0"},
			},
			want: "1.30.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LowestMaxEnvoyVersion(tt.manifests)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveMinimumCompatibleEnvoyVersion(t *testing.T) {
	tests := []struct {
		name      string
		manifests []*Manifest
		want      string
		wantErr   bool
	}{
		{
			name:      "empty list",
			manifests: nil,
			want:      "",
		},
		{
			name: "no version constraints",
			manifests: []*Manifest{
				{Name: "ext1"},
				{Name: "ext2"},
			},
			want: "",
		},
		{
			name: "only min versions - returns highest min",
			manifests: []*Manifest{
				{MinEnvoyVersion: "1.28.0"},
				{MinEnvoyVersion: "1.31.0"},
			},
			want: "1.31.0",
		},
		{
			name: "only max versions - returns lowest max",
			manifests: []*Manifest{
				{MaxEnvoyVersion: "1.35.0"},
				{MaxEnvoyVersion: "1.32.0"},
			},
			want: "1.32.0",
		},
		{
			name: "compatible range - returns highest min",
			manifests: []*Manifest{
				{MinEnvoyVersion: "1.28.0"},
				{MinEnvoyVersion: "1.30.0", MaxEnvoyVersion: "1.35.0"},
				{MaxEnvoyVersion: "1.33.0"},
			},
			want: "1.30.0",
		},
		{
			name: "min equals max - compatible",
			manifests: []*Manifest{
				{MinEnvoyVersion: "1.30.0"},
				{MaxEnvoyVersion: "1.30.0"},
			},
			want: "1.30.0",
		},
		{
			name: "incompatible range - error",
			manifests: []*Manifest{
				{MinEnvoyVersion: "1.33.0"},
				{MaxEnvoyVersion: "1.30.0"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveMinimumCompatibleEnvoyVersion(tt.manifests)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestResolveVersionsMissingParent(t *testing.T) {
	m := &Manifest{
		Name:   "child",
		Type:   TypeGo,
		Parent: "nonexistent-parent",
	}
	err := resolveVersions(m, map[string]*Manifest{})
	require.ErrorIs(t, err, ErrParentManifestNotFound)
}

func TestLoadManifestsDuplicateName(t *testing.T) {
	manifest := `name: duplicate-name
version: 1.0.0
categories: [Security]
author: Test
description: A test extension
longDescription: Test
type: wasm
tags: [test]
license: Apache-2.0
examples: []
`
	fsys := fstest.MapFS{
		"ext1/manifest.yaml": &fstest.MapFile{Data: []byte(manifest)},
		"ext2/manifest.yaml": &fstest.MapFile{Data: []byte(manifest)},
	}
	_, err := LoadManifests(fsys, ".", false)
	require.ErrorIs(t, err, ErrDuplicateManifestName)
}

func TestLoadLocalManifest(t *testing.T) {
	t.Run("valid-manifest", func(t *testing.T) {
		manifestPath := filepath.Join("testdata", "valid_manifest.yaml")
		localManifest, err := LoadLocalManifest(manifestPath)
		require.NoError(t, err)
		require.Equal(t, &Manifest{
			Name:            "test-extension",
			Version:         "1.0.0",
			Categories:      []string{"Security"},
			Author:          "Test Author",
			Description:     "A test extension",
			LongDescription: "This is a longer description of the test extension.\n",
			Type:            TypeWasm,
			FilterType:      FilterTypeHTTP,
			Tags:            []string{"test"},
			License:         "Apache-2.0",
			Examples: []Example{
				{
					Title:       "Basic usage",
					Description: "Run the extension",
					Code:        "boe run --extension test-extension\n",
				},
			},
			Path: manifestPath,
		}, localManifest)
	})

	t.Run("file-not-found", func(t *testing.T) {
		_, err := LoadLocalManifest(filepath.Join("testdata", "nonexistent.yaml"))
		require.ErrorIs(t, err, ErrOpenManifestFile)
	})

	t.Run("invalid-yaml", func(t *testing.T) {
		_, err := LoadLocalManifest(filepath.Join("testdata", "invalid_manifest.yaml"))
		require.ErrorIs(t, err, ErrParseManifestFile)
	})
}

func TestResolveLocalVersions(t *testing.T) {
	t.Run("local-parent-found", func(t *testing.T) {
		// Create a directory structure: parent/child/manifest.yaml
		tmpDir := t.TempDir()
		parentDir := tmpDir
		childDir := filepath.Join(tmpDir, "child")
		require.NoError(t, os.MkdirAll(childDir, 0o750))

		parentManifest := `name: test-parent
version: 9.9.9
composerVersion: 9.9.9
minEnvoyVersion: 1.99.0
type: composer
extensionSet: true
`
		childManifest := `name: test-child
parent: test-parent
categories: [Misc]
author: Test
description: A child extension
longDescription: A child extension
type: go
tags: [test]
license: Apache-2.0
examples: []
`
		require.NoError(t, os.WriteFile(filepath.Join(parentDir, "manifest.yaml"), []byte(parentManifest), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "manifest.yaml"), []byte(childManifest), 0o600))

		m, err := LoadLocalManifest(filepath.Join(childDir, "manifest.yaml"))
		require.NoError(t, err)
		require.Empty(t, m.Version)

		require.NoError(t, ResolveLocalVersions(m))
		assert.Equal(t, "9.9.9", m.Version)
		assert.Equal(t, "9.9.9", m.ComposerVersion)
		assert.Equal(t, "1.99.0", m.MinEnvoyVersion)
		assert.Equal(t, "1.100.0", m.MaxEnvoyVersion) // Automatically computed when loading the manifest
	})

	t.Run("no-local-parent-returns-error", func(t *testing.T) {
		// Create a child manifest in an isolated temp dir (no parent on filesystem).
		tmpDir := t.TempDir()
		childManifest := `name: test-child
parent: composer
categories: [Misc]
author: Test
description: A child extension
longDescription: A child extension
type: go
tags: [test]
license: Apache-2.0
examples: []
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), []byte(childManifest), 0o600))

		m, err := LoadLocalManifest(filepath.Join(tmpDir, "manifest.yaml"))
		require.NoError(t, err)

		// Without the parent manifest on disk, version resolution fails.
		require.ErrorIs(t, ResolveLocalVersions(m), ErrParentManifestNotFound)
	})

	t.Run("noop-for-non-go-type", func(t *testing.T) {
		m := &Manifest{Name: "test", Type: TypeWasm, Version: "1.0.0"}
		require.NoError(t, ResolveLocalVersions(m))
		assert.Equal(t, "1.0.0", m.Version)
	})

	t.Run("noop-for-no-parent", func(t *testing.T) {
		m := &Manifest{Name: "test", Type: TypeGo, Version: "1.0.0"}
		require.NoError(t, ResolveLocalVersions(m))
		assert.Equal(t, "1.0.0", m.Version)
	})
}

func TestValidateNativeHTTPFilters(t *testing.T) {
	validTests := []struct {
		name    string
		fixture string
		expect  *NativeHTTPFilters
	}{
		{
			name:    "valid header-to-metadata before",
			fixture: "native_http_filters_valid.yaml",
			expect: &NativeHTTPFilters{
				Before: []map[string]any{{
					"name": "envoy.filters.http.header_to_metadata",
					"typed_config": map[string]any{
						"@type": "type.googleapis.com/envoy.extensions.filters.http.header_to_metadata.v3.Config",
						"request_rules": []any{
							map[string]any{
								"header": "x-tenant-id",
								"on_header_present": map[string]any{
									"metadata_namespace": "boe.e2e",
									"key":                "tenant",
								},
								"remove": true,
							},
						},
					},
				}},
			},
		},
		{
			name:    "lua with stray filterType accepted",
			fixture: "native_http_filters_lua_with_stray_filter_type.yaml",
			expect: &NativeHTTPFilters{
				Before: []map[string]any{{
					"name": "envoy.filters.http.mcp",
					"typed_config": map[string]any{
						"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
						"traffic_mode": "REJECT_NO_MCP",
					},
				}},
			},
		},
		{
			name:    "valid header-to-metadata after",
			fixture: "native_http_filters_valid_after.yaml",
			expect: &NativeHTTPFilters{
				After: []map[string]any{{
					"name": "envoy.filters.http.header_to_metadata",
					"typed_config": map[string]any{
						"@type": "type.googleapis.com/envoy.extensions.filters.http.header_to_metadata.v3.Config",
						"request_rules": []any{
							map[string]any{
								"header": "x-boe-tenant-metadata",
								"on_header_present": map[string]any{
									"metadata_namespace": "boe.e2e",
									"key":                "tenant",
								},
								"remove": true,
							},
						},
					},
				}},
			},
		},
	}

	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.fixture)
			m, err := LoadLocalManifest(manifestPath)
			require.NoError(t, err)
			require.Equal(t, tt.expect, m.NativeHTTPFilters)
		})
	}

	invalidTests := []struct {
		name        string
		fixture     string
		expectedErr string
	}{
		{
			name:        "router rejected",
			fixture:     "native_http_filters_router_rejected.yaml",
			expectedErr: `validation failed for manifest native_http_filters_router_rejected.yaml: invalid nativeHttpFilters: nativeHttpFilters.before[0]: "envoy.filters.http.router" is appended by BOE and cannot be declared`,
		},
		{
			name:        "mcp router rejected",
			fixture:     "native_http_filters_mcp_router_rejected.yaml",
			expectedErr: `validation failed for manifest native_http_filters_mcp_router_rejected.yaml: invalid nativeHttpFilters: nativeHttpFilters.before[0]: "envoy.filters.http.mcp_router" is a terminal router replacement and is not supported`,
		},
		{
			name:        "duplicate name rejected",
			fixture:     "native_http_filters_duplicate_name.yaml",
			expectedErr: `validation failed for manifest native_http_filters_duplicate_name.yaml: invalid nativeHttpFilters: nativeHttpFilters contains duplicate name "envoy.filters.http.mcp"`,
		},
		{
			name:    "rust network type rejected",
			fixture: "native_http_filters_on_rust_network_type.yaml",
			expectedErr: `validation failed for manifest native_http_filters_on_rust_network_type.yaml: jsonschema validation failed with 'SCHEMA#'
- at '': 'allOf' failed
  - at '/nativeHttpFilters': false schema`,
		},
		{
			name:    "wasm type rejected",
			fixture: "native_http_filters_on_wasm_type.yaml",
			expectedErr: `validation failed for manifest native_http_filters_on_wasm_type.yaml: jsonschema validation failed with 'SCHEMA#'
- at '': 'allOf' failed
  - at '/nativeHttpFilters': false schema`,
		},
		{
			name:    "composer type rejected",
			fixture: "native_http_filters_on_composer_type.yaml",
			expectedErr: `validation failed for manifest native_http_filters_on_composer_type.yaml: jsonschema validation failed with 'SCHEMA#'
- at '': 'allOf' failed
  - at '/nativeHttpFilters': false schema`,
		},
		{
			name:    "missing name rejected",
			fixture: "native_http_filters_missing_name.yaml",
			expectedErr: `validation failed for manifest native_http_filters_missing_name.yaml: jsonschema validation failed with 'SCHEMA#'
- at '/nativeHttpFilters/before/0': missing property 'name'`,
		},
		{
			name:    "missing typed config rejected",
			fixture: "native_http_filters_missing_typed_config.yaml",
		},
		{
			name:    "missing @type rejected",
			fixture: "native_http_filters_missing_at_type.yaml",
		},
		{
			name:        "after: router rejected",
			fixture:     "native_http_filters_router_rejected_after.yaml",
			expectedErr: `validation failed for manifest native_http_filters_router_rejected_after.yaml: invalid nativeHttpFilters: nativeHttpFilters.after[0]: "envoy.filters.http.router" is appended by BOE and cannot be declared`,
		},
		{
			name:        "after: mcp router rejected",
			fixture:     "native_http_filters_mcp_router_rejected_after.yaml",
			expectedErr: `validation failed for manifest native_http_filters_mcp_router_rejected_after.yaml: invalid nativeHttpFilters: nativeHttpFilters.after[0]: "envoy.filters.http.mcp_router" is a terminal router replacement and is not supported`,
		},
		{
			name:        "after: duplicate name rejected",
			fixture:     "native_http_filters_duplicate_name_after.yaml",
			expectedErr: `validation failed for manifest native_http_filters_duplicate_name_after.yaml: invalid nativeHttpFilters: nativeHttpFilters contains duplicate name "envoy.filters.http.mcp_json_rest_bridge"`,
		},
		{
			name:    "after: missing name rejected",
			fixture: "native_http_filters_missing_name_after.yaml",
			expectedErr: `validation failed for manifest native_http_filters_missing_name_after.yaml: jsonschema validation failed with 'SCHEMA#'
- at '/nativeHttpFilters/after/0': missing property 'name'
- at '/nativeHttpFilters/after/0': 'oneOf' failed, none matched
  - at '/nativeHttpFilters/after/0': missing property 'name'
  - at '/nativeHttpFilters/after/0': validation failed
    - at '/nativeHttpFilters/after/0': missing properties 'name', 'config_discovery'
    - at '/nativeHttpFilters/after/0': additional properties 'typed_config' not allowed`,
		},
		{
			name:    "after: missing typed config rejected",
			fixture: "native_http_filters_missing_typed_config_after.yaml",
		},
		{
			name:    "after: missing @type rejected",
			fixture: "native_http_filters_missing_at_type_after.yaml",
		},
	}

	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.fixture)
			_, err := LoadLocalManifest(manifestPath)
			if tt.expectedErr != "" {
				actual := schemaURIPattern.ReplaceAllString(err.Error(), "SCHEMA#")
				require.Equal(t, tt.expectedErr, actual)
			} else {
				require.Error(t, err)
			}
		})
	}
}

var schemaURIPattern = regexp.MustCompile(`file:///[^']+/manifest\.schema\.json#`)

func TestValidateNativeHTTPFiltersSemanticErrors(t *testing.T) {
	tests := []struct {
		name        string
		manifest    *Manifest
		expectedErr string
	}{
		{
			name: "missing name",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					Before: []map[string]any{{}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters.before[0] is missing a name",
		},
		{
			name: "non string name",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					Before: []map[string]any{{
						"name": 123,
					}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters.before[0].name must be a string, got int",
		},
		{
			name: "empty name",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					Before: []map[string]any{{
						"name": "",
					}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters.before[0].name is empty",
		},
		{
			name: "missing http anchor",
			manifest: &Manifest{
				Type: TypeWasm,
				NativeHTTPFilters: &NativeHTTPFilters{
					Before: []map[string]any{{
						"name": "envoy.filters.http.mcp",
					}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters requires an extension that generates an HTTP filter; \"wasm\" with filterType \"\" has no HTTP anchor",
		},
		{
			name: "after: missing name",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					After: []map[string]any{{}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters.after[0] is missing a name",
		},
		{
			name: "after: non string name",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					After: []map[string]any{{
						"name": 123,
					}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters.after[0].name must be a string, got int",
		},
		{
			name: "after: empty name",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					After: []map[string]any{{
						"name": "",
					}},
				},
			},
			expectedErr: "invalid nativeHttpFilters: nativeHttpFilters.after[0].name is empty",
		},
		{
			name: "cross-list duplicate rejected",
			manifest: &Manifest{
				Type: TypeLua,
				NativeHTTPFilters: &NativeHTTPFilters{
					Before: []map[string]any{{
						"name": "envoy.filters.http.mcp",
					}},
					After: []map[string]any{{
						"name": "envoy.filters.http.mcp",
					}},
				},
			},
			expectedErr: `invalid nativeHttpFilters: nativeHttpFilters contains duplicate name "envoy.filters.http.mcp"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNativeHTTPFilters(tt.manifest)
			require.EqualError(t, err, tt.expectedErr)
		})
	}
}

func TestHasHTTPAnchor(t *testing.T) {
	tests := []struct {
		name     string
		manifest *Manifest
		expect   bool
	}{
		{
			name:     "lua",
			manifest: &Manifest{Type: TypeLua},
			expect:   true,
		},
		{
			name:     "go",
			manifest: &Manifest{Type: TypeGo},
			expect:   true,
		},
		{
			name:     "ext proc",
			manifest: &Manifest{Type: TypeExtProc},
			expect:   true,
		},
		{
			name:     "rust default http",
			manifest: &Manifest{Type: TypeRust},
			expect:   true,
		},
		{
			name:     "rust explicit http",
			manifest: &Manifest{Type: TypeRust, FilterType: FilterTypeHTTP},
			expect:   true,
		},
		{
			name:     "rust network",
			manifest: &Manifest{Type: TypeRust, FilterType: FilterTypeNetwork},
			expect:   false,
		},
		{
			name:     "wasm",
			manifest: &Manifest{Type: TypeWasm},
			expect:   false,
		},
		{
			name:     "composer",
			manifest: &Manifest{Type: TypeComposer},
			expect:   false,
		},
		{
			name:     "unknown type",
			manifest: &Manifest{Type: "mystery"},
			expect:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expect, hasHTTPAnchor(tt.manifest))
		})
	}
}
