// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package composer provides built-in plugins for the composer binary.
package composer

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/version"
)

// manifestYAML is the composer extension manifest. It is the single source of truth
// for the composer version: the build reads the very same file to tag the composer
// and the standalone Go plugin images.
//
//go:embed manifest.yaml
var manifestYAML []byte

// manifest is the subset of the composer manifest.yaml that is needed at runtime.
type manifest struct {
	// Version is the composer version, e.g. "0.12.0-dev".
	Version string `yaml:"version"`
}

// init parses the embedded manifest and publishes the composer version so that the
// built-in plugins (e.g. the Go plugin loader) can read it at runtime.
func init() {
	var m manifest
	if err := yaml.Unmarshal(manifestYAML, &m); err != nil {
		// Should never happen: the manifest is embedded at build time.
		panic(fmt.Sprintf("failed to parse embedded composer manifest.yaml: %v", err))
	}
	if m.Version == "" {
		// Should never happen: the build also derives the image tags from this field.
		panic("embedded composer manifest.yaml has no version")
	}
	version.Composer = m.Version
}
