// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"os"
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestRenderDefaultConfig(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config.yaml")
	require.NoError(t, err)

	cfg, err := RenderConfig(&ConfigGenerationParams{
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

	cfg, err := RenderConfig(&ConfigGenerationParams{
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

	cfg, err := RenderConfig(&ConfigGenerationParams{
		Logger:       internaltesting.NewTLogger(t),
		AdminPort:    9901,
		ListenerPort: 10000,
		Extensions:   extensionManifests,
	}, MinimalConfigRenderer)

	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}

func TestParseCluster(t *testing.T) {
	tests := []struct {
		name          string
		spec          string
		expectedError string
		check         func(t *testing.T, c *clusterv3.Cluster)
	}{
		{
			name: "short",
			spec: "example.com:443",
			check: func(t *testing.T, c *clusterv3.Cluster) {
				require.Equal(t, "example.com:443", c.Name)
				require.Equal(t, clusterv3.Cluster_STRICT_DNS, c.GetType())
				ep := c.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint()
				require.Equal(t, "example.com", ep.Address.GetSocketAddress().Address)
				require.Equal(t, uint32(443), ep.Address.GetSocketAddress().GetPortValue())
				require.NotNil(t, c.TransportSocket, "short form should include TLS")
			},
		},
		{
			name: "JSON",
			spec: `{"name":"svc","type":"STRICT_DNS","load_assignment":{"cluster_name":"svc"}}`,
			check: func(t *testing.T, c *clusterv3.Cluster) {
				require.Equal(t, "svc", c.Name)
			},
		},
		{
			name:          "short: invalid missing port",
			spec:          "example.com",
			expectedError: "invalid cluster spec \"example.com\": must be JSON or in the format host:tlsPort",
		},
		{
			name:          "short: invalid bad port",
			spec:          "example.com:abc",
			expectedError: "invalid port in cluster short format: strconv.ParseUint: parsing \"abc\": invalid syntax",
		},
		{
			name:          "JSON invalid",
			spec:          `{"name":}`,
			expectedError: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseCluster(tt.spec)
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				tt.check(t, c)
			}
		})
	}
}

func mustReadManifest(t *testing.T, path string) *extensions.Manifest {
	manifest, err := extensions.LoadLocalManifest(path)
	require.NoError(t, err)
	return manifest
}
