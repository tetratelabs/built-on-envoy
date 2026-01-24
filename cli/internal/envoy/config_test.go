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

func TestRenderDefaultConfig(t *testing.T) {
	want, err := os.ReadFile("testdata/config.yaml")
	require.NoError(t, err)

	cfg, err := buildConfig(9901, 10000, nil)
	require.NoError(t, err)

	cfgYAML, err := ProtoToYaml(cfg)
	require.NoError(t, err)

	require.YAMLEq(t, string(want), string(cfgYAML))
}
