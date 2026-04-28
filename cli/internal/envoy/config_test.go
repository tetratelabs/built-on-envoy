// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"os"
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
	"sigs.k8s.io/yaml"

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

// TestParseNativeHTTPFiltersBeforeRejections covers config-generation errors
// that don't surface at manifest-load time: protojson resolution failures
// (unknown @type, malformed typed_config) and the post-parse rejection of
// a terminal dynamic_modules filter (both snake_case and camelCase spellings).
func TestParseNativeHTTPFiltersBeforeRejections(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		wantErrMsg string
	}{
		{
			name:       "unknown_at_type",
			fixture:    "testdata/input_native_unknown_at_type.yaml",
			wantErrMsg: "example_unknown",
		},
		{
			name:       "malformed_typed_config",
			fixture:    "testdata/input_native_malformed_typed_config.yaml",
			wantErrMsg: "NOT_A_VALID_ENUM_VALUE",
		},
		{
			name:       "dynamic_modules_terminal_snake",
			fixture:    "testdata/input_native_dynmod_terminal_snake.yaml",
			wantErrMsg: "terminal_filter",
		},
		{
			name:       "dynamic_modules_terminal_camel",
			fixture:    "testdata/input_native_dynmod_terminal_camel.yaml",
			wantErrMsg: "terminal_filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mustReadManifest(t, tt.fixture)
			_, err := parseNativeHTTPFiltersBefore(m)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

// TestParseNativeHTTPFiltersBeforeValid round-trips a valid MCP entry.
func TestParseNativeHTTPFiltersBeforeValid(t *testing.T) {
	m := mustReadManifest(t, "../extensions/testdata/native_http_filters_valid.yaml")
	got, err := parseNativeHTTPFiltersBefore(m)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "envoy.filters.http.mcp", got[0].GetName())
	require.NotNil(t, got[0].GetTypedConfig())
}

// TestRenderConfigPreservesGeneratorMultiFilterBracketing locks in the
// composition contract for a generator that returns multiple HTTP filters:
// the chain for a single extension is (before…, generated[0], generated[1]…),
// with the router appended last. No real generator returns >1 today, so we
// exercise the contract via buildHTTPConnectionManager directly.
func TestRenderConfigPreservesGeneratorMultiFilterBracketing(t *testing.T) {
	mustFilter := func(name string) *hcmv3.HttpFilter {
		// Use the router proto (already imported) as a stand-in typed_config.
		typedConfig, err := anypb.New(&routerv3.Router{})
		require.NoError(t, err)
		return &hcmv3.HttpFilter{
			Name:       name,
			ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: typedConfig},
		}
	}

	// Simulate the per-extension composition: before[…] then generated[…].
	composed := []*hcmv3.HttpFilter{
		mustFilter("native.before.0"),
		mustFilter("ext.generated.0"),
		mustFilter("ext.generated.1"),
	}

	hcm, err := buildHTTPConnectionManager(composed, "test-upstream", "")
	require.NoError(t, err)

	got := make([]string, 0, len(hcm.HttpFilters))
	for _, f := range hcm.HttpFilters {
		got = append(got, f.GetName())
	}
	require.Equal(t, []string{
		"native.before.0",
		"ext.generated.0",
		"ext.generated.1",
		"envoy.filters.http.router",
	}, got)
}

// TestParseNativeHTTPFiltersBeforePreservesOrder feeds two before[] entries
// and asserts the parser preserves manifest array order.
func TestParseNativeHTTPFiltersBeforePreservesOrder(t *testing.T) {
	m := &extensions.Manifest{
		Name: "ordered",
		Type: extensions.TypeLua,
		NativeHTTPFilters: &extensions.NativeHTTPFilters{
			Before: []map[string]any{
				{
					"name": "envoy.filters.http.mcp",
					"typed_config": map[string]any{
						"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
						"traffic_mode": "PASS_THROUGH",
					},
				},
				{
					"name": "envoy.filters.http.dynamic_modules",
					"typed_config": map[string]any{
						"@type":       "type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter",
						"filter_name": "n",
						"dynamic_module_config": map[string]any{
							"name": "n",
						},
					},
				},
			},
		},
	}
	got, err := parseNativeHTTPFiltersBefore(m)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "envoy.filters.http.mcp", got[0].GetName())
	require.Equal(t, "envoy.filters.http.dynamic_modules", got[1].GetName())
}

// TestRenderConfigGroupsNativeBeforePerExtension drives the composition end-to-end
// with two extensions, both declaring their own before[]. The HCM chain must
// emit each extension's before[] immediately before that extension's generated
// filter, and the same native filter must appear twice (once per declaring
// extension) — no cross-extension de-duplication.
func TestRenderConfigGroupsNativeBeforePerExtension(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

	mcpBefore := func(mode string) *extensions.NativeHTTPFilters {
		return &extensions.NativeHTTPFilters{Before: []map[string]any{{
			"name": "envoy.filters.http.mcp",
			"typed_config": map[string]any{
				"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
				"traffic_mode": mode,
			},
		}}}
	}

	first := mustReadManifest(t, "testdata/input_lua_inline.yaml")
	first.Name = "ext-one"
	first.NativeHTTPFilters = mcpBefore("REJECT_NO_MCP")

	second := mustReadManifest(t, "testdata/input_lua_inline.yaml")
	second.Name = "ext-two"
	second.NativeHTTPFilters = mcpBefore("PASS_THROUGH")

	cfg, err := RenderConfig(&ConfigGenerationParams{
		Logger:       logger,
		AdminPort:    9901,
		ListenerPort: 10000,
		Extensions:   []*extensions.Manifest{first, second},
	}, MinimalConfigRenderer)
	require.NoError(t, err)

	var payload struct {
		HTTPFilters []struct {
			Name string `json:"name"`
		} `json:"http_filters"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(cfg), &payload))
	names := make([]string, 0, len(payload.HTTPFilters))
	for _, f := range payload.HTTPFilters {
		names = append(names, f.Name)
	}
	require.Equal(t, []string{
		"envoy.filters.http.mcp",
		"ext-one",
		"envoy.filters.http.mcp",
		"ext-two",
	}, names)
}

func TestParseNativeHTTPFiltersBeforeOverride(t *testing.T) {
	mcpJSON := `[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
      "traffic_mode": "PASS_THROUGH"
    }
  }
]`

	tests := []struct {
		name        string
		input       string
		setup       func(t *testing.T) string
		expected    []string
		expectedErr string
	}{
		{
			name:     "valid JSON list",
			input:    mcpJSON,
			expected: []string{"envoy.filters.http.mcp"},
		},
		{
			name: "valid YAML list",
			input: `- name: envoy.filters.http.mcp
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
    traffic_mode: PASS_THROUGH
`,
			expected: []string{"envoy.filters.http.mcp"},
		},
		{
			name: "@filepath",
			setup: func(t *testing.T) string {
				t.Helper()
				fp := t.TempDir() + "/filters.yaml"
				require.NoError(t, os.WriteFile(fp, []byte(mcpJSON), 0o600))
				return "@" + fp
			},
			expected: []string{"envoy.filters.http.mcp"},
		},
		{
			name:        "invalid YAML",
			input:       "not a list",
			expectedErr: "unmarshal filter list: json: cannot unmarshal string into Go value of type []json.RawMessage",
		},
		{
			name: "rejects terminal dynamic module",
			input: `[
  {
    "name": "envoy.filters.http.dynamic_modules",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter",
      "filter_name": "n",
      "terminal_filter": true,
      "dynamic_module_config": { "name": "n" }
    }
  }
]`,
			expectedErr: "entry[0]: envoy.filters.http.dynamic_modules with terminal_filter=true is not supported in nativeHttpFilters.before",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input
			if tt.setup != nil {
				input = tt.setup(t)
			}
			actual, err := parseNativeHTTPFiltersBeforeOverride(input)
			if tt.expectedErr != "" {
				require.EqualError(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			actualNames := make([]string, 0, len(actual))
			for _, f := range actual {
				actualNames = append(actualNames, f.GetName())
			}
			require.Equal(t, tt.expected, actualNames)
		})
	}
}

func TestRenderConfigWithNativeHTTPFiltersBeforeOverride(t *testing.T) {
	tests := []struct {
		name                     string
		manifestNativeHTTPBefore []map[string]any
		nativeHTTPFiltersBefore  []string
		expected                 []string
	}{
		{
			name: "override replaces manifest before",
			manifestNativeHTTPBefore: []map[string]any{{
				"name": "envoy.filters.http.mcp",
				"typed_config": map[string]any{
					"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
					"traffic_mode": "REJECT_NO_MCP",
				},
			}},
			nativeHTTPFiltersBefore: []string{`[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
      "traffic_mode": "PASS_THROUGH"
    }
  }
]`},
			expected: []string{
				"envoy.filters.http.mcp",
				"ext-override-test",
			},
		},
		{
			name: "empty override falls back to manifest",
			manifestNativeHTTPBefore: []map[string]any{{
				"name": "envoy.filters.http.mcp",
				"typed_config": map[string]any{
					"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
					"traffic_mode": "REJECT_NO_MCP",
				},
			}},
			nativeHTTPFiltersBefore: []string{""},
			expected: []string{
				"envoy.filters.http.mcp",
				"ext-override-test",
			},
		},
		{
			name:                     "no manifest before and no override",
			manifestNativeHTTPBefore: nil,
			nativeHTTPFiltersBefore:  nil,
			expected: []string{
				"ext-override-test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := mustReadManifest(t, "testdata/input_lua_inline.yaml")
			ext.Name = "ext-override-test"
			if tt.manifestNativeHTTPBefore != nil {
				ext.NativeHTTPFilters = &extensions.NativeHTTPFilters{Before: tt.manifestNativeHTTPBefore}
			}

			actual, err := RenderConfig(&ConfigGenerationParams{
				Logger:                  internaltesting.NewTLogger(t),
				AdminPort:               9901,
				ListenerPort:            10000,
				Extensions:              []*extensions.Manifest{ext},
				NativeHTTPFiltersBefore: tt.nativeHTTPFiltersBefore,
			}, MinimalConfigRenderer)
			require.NoError(t, err)

			var payload struct {
				HTTPFilters []struct {
					Name string `json:"name"`
				} `json:"http_filters"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(actual), &payload))
			actualNames := make([]string, 0, len(payload.HTTPFilters))
			for _, f := range payload.HTTPFilters {
				actualNames = append(actualNames, f.Name)
			}
			require.Equal(t, tt.expected, actualNames)
		})
	}
}
