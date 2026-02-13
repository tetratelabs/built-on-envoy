// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package composer_test

import (
	"embed"
	"io/fs"
	"testing"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//go:embed */manifest.yaml
var manifestFiles embed.FS

// Manifest represents the minimal structure needed for validation
type Manifest struct {
	Name            string `yaml:"name"`
	Version         string `yaml:"version,omitempty"`
	Type            string `yaml:"type"`
	ComposerVersion string `yaml:"composerVersion,omitempty"`
	Parent          string `yaml:"parent,omitempty"`
}

// TestManifestValidation validates critical fields in all embedded manifest.yaml files
func TestManifestValidation(t *testing.T) {
	// Find all manifest.yaml files in embedded FS
	var manifestPaths []string
	err := fs.WalkDir(manifestFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "manifest.yaml" {
			manifestPaths = append(manifestPaths, path)
		}
		return nil
	})
	require.NoError(t, err, "Failed to walk embedded files")

	t.Logf("Found %d embedded manifest.yaml files", len(manifestPaths))
	t.Logf("Manifest paths: %v", manifestPaths)

	knownExtensions := map[string]bool{}
	for _, manifestPath := range manifestPaths {
		t.Run(manifestPath, func(t *testing.T) {
			validateManifest(t, manifestPath, knownExtensions)
		})
	}
}

func validateManifest(t *testing.T, path string, knownExtensions map[string]bool) {
	// Read the manifest file from embedded FS
	data, err := manifestFiles.ReadFile(path)
	require.NoError(t, err, "Failed to read embedded manifest")

	// Parse YAML
	var manifest Manifest
	err = yaml.Unmarshal(data, &manifest)
	require.NoError(t, err, "Failed to parse YAML")

	// Validate name
	require.NotEmpty(t, manifest.Name, "name is required")
	require.False(t, knownExtensions[manifest.Name], "duplicate extension name '%s'", manifest.Name)
	knownExtensions[manifest.Name] = true
	require.NotNil(t, sdk.GetHttpFilterConfigFactory(manifest.Name), "plugin '%s' is not registered into the binary, please ensure it is properly initialized or imported in the plugins.go", manifest.Name)

	// Validate type
	require.Equal(t, "composer", manifest.Type, "type must be 'composer'")

	// Validate version.
	require.Empty(t, manifest.Version, "in-tree composer plugins should not have a version")

	require.Empty(t, manifest.ComposerVersion, "in-tree composer plugins should not have a composerVersion")

	// Validate parent.
	require.Equal(t, "composer", manifest.Parent, "composer plugins must have parent composer")
}
