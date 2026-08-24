// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package composer_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/version"
)

// TestComposerVersion verifies the composer package init published the version of the
// embedded manifest.yaml, which is the same file the build derives the image tags from.
func TestComposerVersion(t *testing.T) {
	data, err := os.ReadFile("manifest.yaml")
	require.NoError(t, err)

	var m struct {
		Version string `yaml:"version"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))
	require.NotEmpty(t, m.Version, "the composer manifest must have a version")

	assert.Equal(t, m.Version, version.Composer)
}
