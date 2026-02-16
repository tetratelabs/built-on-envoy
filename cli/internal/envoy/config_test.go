// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestRenderDefaultConfig(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config.yaml")
	require.NoError(t, err)

	cfg, err := RenderConfig(ConfigGenerationParams{
		Logger:       internaltesting.NewTLogger(t),
		AdminPort:    9901,
		ListenerPort: 10000,
	}, FullConfigRenderer)
	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}

func TestRenderConfigWithExtensions(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config_with_extensions.yaml")
	require.NoError(t, err)

	extensionManifests := []*extensions.Manifest{
		mustReadManifest(t, "testdata/input_lua_inline.yaml"),
	}

	cfg, err := RenderConfig(ConfigGenerationParams{
		Logger:       internaltesting.NewTLogger(t),
		AdminPort:    9901,
		ListenerPort: 10000,
		Extensions:   extensionManifests,
	}, FullConfigRenderer)

	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}

func TestRenderMinimalConfigWithExtensions(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config_only_filters.yaml")
	require.NoError(t, err)

	extensionManifests := []*extensions.Manifest{
		mustReadManifest(t, "testdata/input_lua_inline.yaml"),
	}

	cfg, err := RenderConfig(ConfigGenerationParams{
		Logger:       internaltesting.NewTLogger(t),
		AdminPort:    9901,
		ListenerPort: 10000,
		Extensions:   extensionManifests,
	}, MinimalConfigRenderer)

	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}

func mustReadManifest(t *testing.T, path string) *extensions.Manifest {
	manifest, err := extensions.LoadLocalManifest(path)
	require.NoError(t, err)
	return manifest
}
