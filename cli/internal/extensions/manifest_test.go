// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllManifestsAreValid(t *testing.T) {
	_, err := loadManifests(manifestFS, true)
	require.NoError(t, err)
}

func TestAllManifestsAreLoaded(t *testing.T) {
	count := 0
	err := fs.WalkDir(manifestFS, "manifests", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) == "manifest.yaml" {
			count++
		}
		return nil
	})

	require.NoError(t, err)
	require.Len(t, Manifests, count)
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

func TestManifestsForCatalog(t *testing.T) {
	manifests := ManifestsIndex()
	require.Less(t, len(manifests), len(Manifests))
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
		"manifests/ext1/manifest.yaml": &fstest.MapFile{Data: []byte(manifest)},
		"manifests/ext2/manifest.yaml": &fstest.MapFile{Data: []byte(manifest)},
	}
	_, err := loadManifests(fsys, false)
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
	})

	t.Run("no-local-parent-falls-back-to-embedded", func(t *testing.T) {
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

		// Should fall back to embedded manifests (composer exists in embedded).
		require.NoError(t, ResolveLocalVersions(m))
		assert.Equal(t, Manifests["composer"].Version, m.Version)
		assert.Equal(t, Manifests["composer"].Version, m.ComposerVersion)
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
