// Copyright Built On Envoy
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
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

type (
	// Manifest represents the metadata of an Envoy extension.
	Manifest struct {
		Name    string `yaml:"name" json:"name"`
		Version string `yaml:"version,omitempty" json:"version,omitempty"`
		// Parent references a parent extension whose version will be used.
		// When set, the version field can be omitted.
		Parent string `yaml:"parent,omitempty" json:"parent,omitempty"`
		// ExtensionSet indicates this manifest defines a set of extensions.
		ExtensionSet    bool      `yaml:"extensionSet,omitempty" json:"extensionSet,omitempty"`
		Categories      []string  `yaml:"categories" json:"categories"`
		Author          string    `yaml:"author" json:"author"`
		Featured        bool      `yaml:"featured" json:"featured,omitempty"`
		Description     string    `yaml:"description" json:"description"`
		LongDescription string    `yaml:"longDescription" json:"longDescription"`
		Type            Type      `yaml:"type" json:"type"`
		Tags            []string  `yaml:"tags" json:"tags"`
		License         string    `yaml:"license" json:"license"`
		Examples        []Example `yaml:"examples" json:"examples"`
		MinEnvoyVersion string    `yaml:"minEnvoyVersion,omitempty" json:"minEnvoyVersion,omitempty"`
		MaxEnvoyVersion string    `yaml:"maxEnvoyVersion,omitempty" json:"maxEnvoyVersion,omitempty"`

		// ComposerVersion specifies the compatible Composer dynamic module version
		// for Composer go plugins.
		ComposerVersion string `yaml:"composerVersion,omitempty" json:"composerVersion,omitempty"`
		Lua             *Lua   `yaml:"lua,omitempty" json:"lua,omitempty"`

		// Path to the manifest file in the local filesystem.
		Path string `yaml:"-" json:"-"`
		// Remote indicates whether this manifest is from a remote extension.
		// This is set by the extension Downloader when fetching remote manifests.
		Remote bool `yaml:"-" json:"-"`
		// SourceRegistry is the OCI registry the extension was fetched from.
		// Set by the Downloader for remote extensions.
		SourceRegistry string `yaml:"-" json:"-"`
		// SourceTag is the resolved version tag the extension was fetched with.
		// Set by the Downloader for remote extensions.
		SourceTag string `yaml:"-" json:"-"`
	}

	// Example represents an example usage of an extension.
	Example struct {
		Title       string `yaml:"title" json:"title"`
		Description string `yaml:"description" json:"description"`
		Code        string `yaml:"code" json:"code"`
	}

	// Type represents the type of an Envoy extension.
	Type string

	// Lua configuration for manifests that define Lua extensions
	Lua struct {
		Inline string `yaml:"inline,omitempty" json:"inline,omitempty"`
		Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	}

	// ManifestIndexEntry represents manifest entry in the manifext index JSON that is used
	// as the source of truth of manifests and served in the public site.
	ManifestIndexEntry struct {
		*Manifest  `yaml:",inline" json:",inline"`
		SourcePath string `yaml:"sourcePath" json:"sourcePath"`
	}
)

// SupportsEnvoyVersion checks if the extension supports the given Envoy version.
func (m *Manifest) SupportsEnvoyVersion(version string) bool {
	envoySemver := "v" + version
	if m.MinEnvoyVersion != "" && semver.Compare(envoySemver, "v"+m.MinEnvoyVersion) < 0 {
		return false
	}
	if m.MaxEnvoyVersion != "" && semver.Compare(envoySemver, "v"+m.MaxEnvoyVersion) > 0 {
		return false
	}
	return true
}

// EnvoyConstraints returns a human-readable string of Envoy version constraints.
func (m *Manifest) EnvoyConstraints() string {
	constraints := ""
	if m.MinEnvoyVersion != "" {
		constraints += fmt.Sprintf(">= %s", m.MinEnvoyVersion)
	}
	if m.MaxEnvoyVersion != "" {
		if constraints != "" {
			constraints += " && "
		}
		constraints += fmt.Sprintf("<= %s", m.MaxEnvoyVersion)
	}
	return constraints
}

const (
	// TypeLua represents a Lua extension.
	TypeLua Type = "lua"
	// TypeWasm represents a Wasm extension.
	TypeWasm Type = "wasm"
	// TypeRust represents a Rust extension.
	TypeRust Type = "rust"
	// TypeGo represents a Go extension.
	TypeGo Type = "go"
	// TypeComposer represents a Composer extension that bundles together
	// multiple Go extensions.
	TypeComposer Type = "composer"

	schemaURL = "manifest.schema.json"
)

var (
	//go:embed manifests
	manifestFS embed.FS

	//go:embed manifests/manifest.schema.json
	manifestSchemaFile []byte
	manifestSchema     *jsonschema.Schema

	// Manifests contains all loaded extension manifests.
	Manifests map[string]*Manifest
)

var (
	// ErrDuplicateManifestName is returned when there are duplicate manifest names.
	ErrDuplicateManifestName = fmt.Errorf("duplicate manifest name")
	// ErrOpenManifestFile is returned when a manifest file cannot be opened.
	ErrOpenManifestFile = fmt.Errorf("failed to open manifest file")
	// ErrReadManifestFile is returned when a manifest file cannot be read.
	ErrReadManifestFile = fmt.Errorf("failed to read manifest file")
	// ErrParseManifestFile is returned when a manifest file cannot be parsed.
	ErrParseManifestFile = fmt.Errorf("failed to parse manifest file")
	// ErrParentManifestNotFound is returned when a parent manifest cannot be found.
	ErrParentManifestNotFound = fmt.Errorf("parent manifest not found")
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

	Manifests, err = loadManifests(manifestFS, false)
	if err != nil {
		panic(err)
	}
}

// loadManifests walks the filesystem and loads all manifest.yaml files.
func loadManifests(fsys fs.FS, validate bool) (map[string]*Manifest, error) {
	result := make(map[string]*Manifest)
	err := fs.WalkDir(fsys, "manifests", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "manifest.yaml" {
			return nil
		}
		m, err := loadManifest(fsys, path, validate)
		if err != nil {
			return err
		}
		if _, ok := result[m.Name]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateManifestName, m.Name)
		}
		m.Path = path
		result[m.Name] = m
		return nil
	})
	if err != nil {
		return nil, err
	}

	// For composer manifests that have a parent, resolve the version and composer versions
	for _, m := range result {
		if err := resolveVersions(m, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// loadManifest loads a manifest from the given filesystem and path.
func loadManifest(fsys fs.FS, path string, validate bool) (*Manifest, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrOpenManifestFile, path)
	}
	defer func() { _ = f.Close() }()

	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrReadManifestFile, path)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrParseManifestFile, path)
	}

	if validate {
		if err := ValidateManifest(&m); err != nil {
			return nil, fmt.Errorf("validation failed for manifest %s: %w", path, err)
		}
	}

	return &m, nil
}

// resolveVersions resolves the version and composer version for a manifest if it has a parent.
func resolveVersions(m *Manifest, all map[string]*Manifest) error {
	if m.Type == TypeGo && m.Parent != "" {
		parent, ok := all[m.Parent]
		if !ok {
			return fmt.Errorf("%w: %s", ErrParentManifestNotFound, m.Parent)
		}
		if m.Version == "" {
			m.Version = parent.Version
		}
		if m.ComposerVersion == "" {
			m.ComposerVersion = parent.Version
		}
		if m.MinEnvoyVersion == "" {
			m.MinEnvoyVersion = parent.MinEnvoyVersion
		}
		if m.MaxEnvoyVersion == "" {
			m.MaxEnvoyVersion = parent.MaxEnvoyVersion
		}
	}
	return nil
}

// ManifestsIndex returns a list of manifests that should be included in the catalog.
// This filters out manifests that are only used as parents for version inheritance.
func ManifestsIndex() []*ManifestIndexEntry {
	manifests := make([]*ManifestIndexEntry, 0, len(Manifests))
	for _, m := range Manifests {
		if !m.ExtensionSet {
			manifests = append(manifests, &ManifestIndexEntry{
				Manifest:   m,
				SourcePath: filepath.Dir(strings.TrimPrefix(m.Path, "manifests/")),
			})
		}
	}
	slices.SortFunc(manifests, func(a, b *ManifestIndexEntry) int {
		return strings.Compare(a.Name, b.Name)
	})
	return manifests
}

// LoadLocalManifest loads a manifest from the given file path.
func LoadLocalManifest(path string) (*Manifest, error) {
	m, err := loadManifest(os.DirFS(filepath.Dir(path)), filepath.Base(path), true)
	if err != nil {
		return nil, err
	}
	m.Path = path
	return m, nil
}

// ResolveLocalVersions resolves version fields for a local manifest that has a parent.
// It first tries to find the parent manifest on the local filesystem by walking up the
// directory tree from the manifest's path. If not found locally, it falls back to the
// embedded manifests.
func ResolveLocalVersions(m *Manifest) error {
	if m.Type != TypeGo || m.Parent == "" {
		return nil
	}

	// Try to find the parent manifest on the local filesystem.
	parent, err := findLocalParentManifest(m)
	if err == nil {
		return resolveVersions(m, map[string]*Manifest{parent.Name: parent})
	}

	// Fall back to the embedded manifests.
	return resolveVersions(m, Manifests)
}

// findLocalParentManifest walks up the directory tree from the manifest's path
// looking for a manifest.yaml whose name matches the parent field.
func findLocalParentManifest(m *Manifest) (*Manifest, error) {
	// Start from the directory containing the child manifest and walk up.
	dir := filepath.Dir(m.Path)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding the parent.
			return nil, fmt.Errorf("%w: %s", ErrParentManifestNotFound, m.Parent)
		}
		dir = parent

		candidate := filepath.Join(dir, "manifest.yaml")
		if _, err := os.Stat(candidate); err != nil {
			continue
		}

		// Load without validation since the parent may have a different type (e.g. composer).
		cm, err := loadManifest(os.DirFS(dir), "manifest.yaml", false)
		if err != nil {
			continue
		}
		if cm.Name == m.Parent {
			return cm, nil
		}
	}
}

// ValidateManifest validates the manifest against the JSON schema.
func ValidateManifest(manifest *Manifest) error {
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

// HighestMinEnvoyVersion returns the highest minimum Envoy version among the given manifests.
func HighestMinEnvoyVersion(manifests []*Manifest) string {
	highest := "0.0.0"
	for _, m := range manifests {
		if m.MinEnvoyVersion != "" && semver.Compare("v"+m.MinEnvoyVersion, "v"+highest) > 0 {
			highest = m.MinEnvoyVersion
		}
	}
	if highest == "0.0.0" {
		return ""
	}
	return highest
}

// LowestMaxEnvoyVersion returns the lowest maximum Envoy version among the given manifests.
func LowestMaxEnvoyVersion(manifests []*Manifest) string {
	lowest := "9999.9999.9999"
	for _, m := range manifests {
		if m.MaxEnvoyVersion != "" && semver.Compare("v"+m.MaxEnvoyVersion, "v"+lowest) < 0 {
			lowest = m.MaxEnvoyVersion
		}
	}
	if lowest == "9999.9999.9999" {
		return ""
	}
	return lowest
}

// ResolveMinimumCompatibleEnvoyVersion returns the minimum Envoy version that is compatible with all given manifests.
func ResolveMinimumCompatibleEnvoyVersion(manifests []*Manifest) (string, error) {
	highestMin := HighestMinEnvoyVersion(manifests)
	lowestMax := LowestMaxEnvoyVersion(manifests)

	if highestMin != "" && lowestMax != "" && semver.Compare("v"+highestMin, "v"+lowestMax) > 0 {
		return "", fmt.Errorf("no compatible Envoy version found: highest minimum %s, lowest maximum %s", highestMin, lowestMax)
	}

	if highestMin != "" {
		return highestMin, nil
	}
	if lowestMax != "" {
		return lowestMax, nil
	}
	return "", nil
}
