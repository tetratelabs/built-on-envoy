// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfigTemplateIsNotEmpty(t *testing.T) {
	require.NotEmpty(t, defaultConfig)
	require.Contains(t, defaultConfig, "{{ .AdminPort }}")
	require.Contains(t, defaultConfig, "{{ .ListenerPort }}")
}

func TestRenderConfig(t *testing.T) {
	rendered, err := RenderConfig(ConfigGenerationParams{
		AdminPort:    9901,
		ListenerPort: 10000,
	})
	require.NoError(t, err)

	want, err := os.ReadFile("testdata/default-config.yaml")
	require.NoError(t, err)
	require.YAMLEq(t, string(want), rendered)
}
