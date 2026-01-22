// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Synchronize the manifests so they can be go:embed-d in the CLI binary.
//go:generate sh sync-manifests.sh

// Package extensions defines types for managing extension manifests.
package extensions

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

type (
	// Manifest represents the metadata of an Envoy extension.
	Manifest struct {
		Name            string    `yaml:"name" json:"name"`
		Version         string    `yaml:"version" json:"version"`
		Categories      []string  `yaml:"categories" json:"categories"`
		Author          string    `yaml:"author" json:"author"`
		Featured        bool      `yaml:"featured" json:"featured,omitempty"`
		Description     string    `yaml:"description" json:"description"`
		LongDescription string    `yaml:"longDescription" json:"longDescription"`
		Type            Type      `yaml:"type" json:"type"`
		Tags            []string  `yaml:"tags" json:"tags"`
		License         string    `yaml:"license" json:"license"`
		Examples        []Example `yaml:"examples" json:"examples"`
	}

	// Example represents an example usage of an extension.
	Example struct {
		Title       string `yaml:"title" json:"title"`
		Description string `yaml:"description" json:"description"`
		Code        string `yaml:"code" json:"code"`
	}

	// Type represents the type of an Envoy extension.
	Type string
)

const (
	// TypeLua represents a Lua extension.
	TypeLua Type = "lua"
	// TypeWasm represents a Wasm extension.
	TypeWasm Type = "wasm"
	// TypeDynamicModule represents a Dynamic Module extension.
	TypeDynamicModule Type = "dynamic_module"
	// TypeComposer represents a Composer extension.
	TypeComposer Type = "composer"

	schemaURL = "manifest.schema.json"
)

var (
	//go:embed manifests/**/*.yaml
	manifestFS embed.FS

	//go:embed manifests/manifest.schema.json
	manifestSchemaFile []byte
	manifestSchema     *jsonschema.Schema

	// Manifests contains all loaded extension manifests.
	Manifests map[string]*Manifest
)

func init() {
	// Parse the schema JSON
	var schemaDoc any
	if err := json.Unmarshal(manifestSchemaFile, &schemaDoc); err != nil {
		panic(fmt.Errorf("failed to parse manifest schema JSON: %w", err))
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaURL, schemaDoc); err != nil {
		panic(fmt.Errorf("failed to load manifest schema resource: %w", err))
	}

	var err error
	manifestSchema, err = compiler.Compile(schemaURL)
	if err != nil {
		panic(fmt.Errorf("failed to compile manifest schema: %w", err))
	}

	Manifests, err = loadManifests(manifestFS)
	if err != nil {
		panic(err)
	}
}

// loadManifests walks the embedded filesystem and loads all manifest.yaml files.
func loadManifests(fsys embed.FS) (map[string]*Manifest, error) {
	result := make(map[string]*Manifest)
	err := fs.WalkDir(fsys, "manifests", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "manifest.yaml" {
			return nil
		}
		data, err := fsys.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read manifest file %s: %w", path, err)
		}
		var m Manifest
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("failed to unmarshal manifest file %s: %w", path, err)
		}
		result[m.Name] = &m
		return nil
	})
	return result, err
}

// validateManifest validates the manifest against the JSON schema.
func validateManifest(manifest *Manifest) error {
	// Convert manifest to JSON for schema validation
	jsonData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest to JSON: %w", err)
	}
	// Unmarshal into any for schema validation
	var v any
	if err := json.Unmarshal(jsonData, &v); err != nil {
		return fmt.Errorf("failed to unmarshal JSON for validation: %w", err)
	}

	return manifestSchema.Validate(v)
}
