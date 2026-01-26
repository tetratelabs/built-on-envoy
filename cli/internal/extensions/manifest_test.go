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
	err := fs.WalkDir(manifestFS, "manifests", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})

	require.NoError(t, err)
	require.Len(t, Manifests, count)
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
