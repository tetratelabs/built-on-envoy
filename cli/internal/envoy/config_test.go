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
)

func TestRenderDefaultConfig(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config.yaml")
	require.NoError(t, err)

	cfg, err := RenderConfig(ConfigGenerationParams{
		AdminPort:    9901,
		ListenerPort: 10000,
	})
	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}

func TestRenderConfigWithExtensions(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config_with_extensions.yaml")
	require.NoError(t, err)

	extensionManifests := []*extensions.Manifest{
		{
			Name: "lua-inline",
			Type: extensions.TypeLua,
			Lua:  &extensions.Lua{Inline: `test`},
		},
	}

	cfg, err := RenderConfig(ConfigGenerationParams{
		AdminPort:    9901,
		ListenerPort: 10000,
		Extensions:   extensionManifests,
	})
	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}
