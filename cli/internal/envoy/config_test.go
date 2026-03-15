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
		tls           bool
		expectedError string
		check         func(t *testing.T, c *clusterv3.Cluster)
	}{
		{
			name: "tls: short",
			spec: "example.com:443",
			tls:  true,
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
			name:          "tls short: invalid missing port",
			spec:          "example.com",
			tls:           true,
			expectedError: "invalid cluster spec \"example.com\": must be in the format host:tlsPort",
		},
		{
			name:          "tls short: invalid bad port",
			spec:          "example.com:abc",
			tls:           true,
			expectedError: "invalid port in cluster short format: strconv.ParseUint: parsing \"abc\": invalid syntax",
		},
		{
			name: "insecure: short",
			spec: "example.com:80",
			tls:  false,
			check: func(t *testing.T, c *clusterv3.Cluster) {
				require.Equal(t, "example.com:80", c.Name)
				require.Equal(t, clusterv3.Cluster_STRICT_DNS, c.GetType())
				ep := c.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint()
				require.Equal(t, "example.com", ep.Address.GetSocketAddress().Address)
				require.Equal(t, uint32(80), ep.Address.GetSocketAddress().GetPortValue())
				require.Nil(t, c.TransportSocket, "short form should not include TLS")
			},
		},
		{
			name:          "insecure short: invalid missing port",
			spec:          "example.com",
			tls:           false,
			expectedError: "invalid cluster spec \"example.com\": must be in the format host:port",
		},
		{
			name:          "insecure short: invalid bad port",
			spec:          "example.com:abc",
			tls:           false,
			expectedError: "invalid port in cluster short format: strconv.ParseUint: parsing \"abc\": invalid syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseCluster(tt.spec, tt.tls)
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				tt.check(t, c)
			}
		})
	}
}

func TestParseJSONCluster(t *testing.T) {
	tests := []struct {
		name          string
		spec          string
		expectedError string
		check         func(t *testing.T, c *clusterv3.Cluster)
	}{
		{
			name: "JSON",
			spec: `{"name":"svc","type":"STRICT_DNS","load_assignment":{"cluster_name":"svc"}}`,
			check: func(t *testing.T, c *clusterv3.Cluster) {
				require.Equal(t, "svc", c.Name)
			},
		},
		{
			name:          "JSON invalid",
			spec:          `{"name":}`,
			expectedError: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseJSONCluster(tt.spec)
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				tt.check(t, c)
			}
		})
	}
}

func TestRenderConfigWithTestUpstreamCluster(t *testing.T) {
	want, err := os.ReadFile("testdata/output_config_with_test_upstream_cluster.yaml")
	require.NoError(t, err)

	cfg, err := RenderConfig(&ConfigGenerationParams{
		Logger:              internaltesting.NewTLogger(t),
		AdminPort:           9901,
		ListenerPort:        10000,
		Clusters:            []string{"example.com:443"},
		TestUpstreamCluster: "example.com:443",
	}, FullConfigRenderer)
	require.NoError(t, err)
	require.YAMLEq(t, string(want), cfg)
}

func TestRenderConfigWithTestUpstreamClusterNotFound(t *testing.T) {
	_, err := RenderConfig(&ConfigGenerationParams{
		Logger:              internaltesting.NewTLogger(t),
		AdminPort:           9901,
		ListenerPort:        10000,
		TestUpstreamCluster: "nonexistent-cluster",
	}, FullConfigRenderer)
	require.ErrorContains(t, err, `cluster "nonexistent-cluster" specified via --test-upstream-cluster does not exist`)
}

func mustReadManifest(t *testing.T, path string) *extensions.Manifest {
	manifest, err := extensions.LoadLocalManifest(path)
	require.NoError(t, err)
	return manifest
}
