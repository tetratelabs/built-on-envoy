// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllManifestsAreValid(t *testing.T) {
	for _, manifest := range Manifests {
		assert.NoErrorf(t, ValidateManifest(manifest), "manifest: %s", manifest.Name)
	}
}

func TestAllMAnifestsAreLoaded(t *testing.T) {
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

func TestValidateComposerManifest(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"composer_empty_version.yaml", "", true},
		{"composer_invalid_version.yaml", "", true},
		{"composer_missing_version.yaml", "", true},
		{"composer_valid.yaml", "1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.name)
			localManifest, err := LoadLocalManifest(manifestPath)
			require.NoError(t, err)

			err = ValidateManifest(localManifest)
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
			require.NoError(t, err)

			err = ValidateManifest(localManifest)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, localManifest.Lua)
			}
		})
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
		{"parent_valid_with_version.yaml", false},
		{"parent_missing_version.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", tt.name)
			localManifest, err := LoadLocalManifest(manifestPath)
			require.NoError(t, err)

			err = ValidateManifest(localManifest)
			if tt.wantErr {
				require.Error(t, err, "manifest: %s", localManifest.Name)
			} else {
				require.NoError(t, err, "manifest: %s", localManifest.Name)
			}
		})
	}
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
