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
			Type:            TypeLua,
			Tags:            []string{"test"},
			License:         "Apache-2.0",
			Examples: []Example{
				{
					Title:       "Basic usage",
					Description: "Run the extension",
					Code:        "boe run --plugin test-extension\n",
				},
			},
			Path: manifestPath,
		}, localManifest)
	})

	t.Run("file-not-found", func(t *testing.T) {
		_, err := LoadLocalManifest(filepath.Join("testdata", "nonexistent.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read manifest file")
	})

	t.Run("invalid-yaml", func(t *testing.T) {
		_, err := LoadLocalManifest(filepath.Join("testdata", "invalid_manifest.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal manifest file")
	})
}
