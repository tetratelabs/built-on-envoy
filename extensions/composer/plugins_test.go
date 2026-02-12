// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package composer

import (
	"embed"
	"io/fs"
	"testing"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
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
	if err != nil {
		t.Fatalf("Failed to walk embedded files: %v", err)
	}

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
	if err != nil {
		t.Fatalf("Failed to read embedded manifest: %v", err)
	}

	// Parse YAML
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	// Validate name
	if manifest.Name == "" {
		t.Error("name is required")
	} else {
		if knownExtensions[manifest.Name] {
			t.Errorf("duplicate extension name '%s'", manifest.Name)
		}
		knownExtensions[manifest.Name] = true
		if sdk.GetHttpFilterConfigFactory(manifest.Name) == nil {
			t.Errorf("plugin '%s' is not registered into the binary, please ensure it is properly initialized or imported in the plugins.go", manifest.Name)
		}
	}

	// Validate type
	if manifest.Type != "composer" {
		t.Errorf("type '%s' must be 'composer'", manifest.Type)
	}

	// Validate version.
	if manifest.Version != "" {
		t.Errorf("in-tree composer plugins should not have a version: '%s'", manifest.Version)
	}

	if manifest.ComposerVersion != "" {
		t.Errorf("in-tree composer plugins should not have a composerVersion: '%s'", manifest.ComposerVersion)
	}

	// Validate parent.
	if manifest.Parent != "composer" {
		t.Errorf("composer plugins must have parent composer")
	}
}
