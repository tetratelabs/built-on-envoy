// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	dymv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/dynamic_modules/v3"
	dymhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_modules/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"sigs.k8s.io/yaml"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

//go:embed testdata/output_config.yaml
var outputConfigYAML []byte

//go:embed testdata/output_config_with_extensions.yaml
var outputConfigWithExtensionsYAML []byte

//go:embed testdata/output_config_only_filters.yaml
var outputConfigOnlyFiltersYAML []byte

//go:embed testdata/output_config_with_test_upstream_cluster.yaml
var outputConfigWithTestUpstreamClusterYAML []byte

func TestRenderConfig(t *testing.T) {
	manifest, err := extensions.LoadLocalManifest("testdata/input_lua_inline.yaml")
	require.NoError(t, err)

	tests := []struct {
		name        string
		params      *ConfigGenerationParams
		renderer    ConfigRenderer
		expect      string
		expectedErr string
	}{
		{
			name: "default",
			params: &ConfigGenerationParams{
				Logger:       internaltesting.NewTLogger(t),
				AdminPort:    9901,
				ListenerPort: 10000,
			},
			renderer: FullConfigRenderer,
			expect:   string(outputConfigYAML),
		},
		{
			name: "with extensions",
			params: &ConfigGenerationParams{
				Logger:       internaltesting.NewTLogger(t),
				AdminPort:    9901,
				ListenerPort: 10000,
				Extensions:   []*extensions.Manifest{manifest},
			},
			renderer: FullConfigRenderer,
			expect:   string(outputConfigWithExtensionsYAML),
		},
		{
			name: "minimal with extensions",
			params: &ConfigGenerationParams{
				Logger:       internaltesting.NewTLogger(t),
				AdminPort:    9901,
				ListenerPort: 10000,
				Extensions:   []*extensions.Manifest{manifest},
			},
			renderer: MinimalConfigRenderer,
			expect:   string(outputConfigOnlyFiltersYAML),
		},
		{
			name: "with test upstream cluster",
			params: &ConfigGenerationParams{
				Logger:              internaltesting.NewTLogger(t),
				AdminPort:           9901,
				ListenerPort:        10000,
				Clusters:            []string{"example.com:443"},
				TestUpstreamCluster: "example.com:443",
			},
			renderer: FullConfigRenderer,
			expect:   string(outputConfigWithTestUpstreamClusterYAML),
		},
		{
			name: "test upstream cluster not found",
			params: &ConfigGenerationParams{
				Logger:              internaltesting.NewTLogger(t),
				AdminPort:           9901,
				ListenerPort:        10000,
				TestUpstreamCluster: "nonexistent-cluster",
			},
			renderer:    FullConfigRenderer,
			expectedErr: `failed to render config: cluster "nonexistent-cluster" specified via --test-upstream-cluster does not exist; configure it with --cluster, --cluster-insecure, or --cluster-json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderConfig(tt.params, tt.renderer)
			if tt.expectedErr != "" {
				requireEqualError(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
				require.YAMLEq(t, tt.expect, result)
			}
		})
	}
}

func TestParseCluster(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		tls         bool
		expectedErr string
		check       func(t *testing.T, c *clusterv3.Cluster)
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
			name:        "tls short: invalid missing port",
			spec:        "example.com",
			tls:         true,
			expectedErr: "invalid cluster spec \"example.com\": must be in the format host:tlsPort",
		},
		{
			name:        "tls short: invalid bad port",
			spec:        "example.com:abc",
			tls:         true,
			expectedErr: "invalid port in cluster short format: strconv.ParseUint: parsing \"abc\": invalid syntax",
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
			name:        "insecure short: invalid missing port",
			spec:        "example.com",
			tls:         false,
			expectedErr: "invalid cluster spec \"example.com\": must be in the format host:port",
		},
		{
			name:        "insecure short: invalid bad port",
			spec:        "example.com:abc",
			tls:         false,
			expectedErr: "invalid port in cluster short format: strconv.ParseUint: parsing \"abc\": invalid syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseCluster(tt.spec, tt.tls)
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
				tt.check(t, c)
			}
		})
	}
}

func TestParseJSONCluster(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		expectedErr string
		check       func(t *testing.T, c *clusterv3.Cluster)
	}{
		{
			name: "JSON",
			spec: `{"name":"svc","type":"STRICT_DNS","load_assignment":{"cluster_name":"svc"}}`,
			check: func(t *testing.T, c *clusterv3.Cluster) {
				require.Equal(t, "svc", c.Name)
			},
		},
		{
			name:        "JSON invalid",
			spec:        `{"name":}`,
			expectedErr: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseJSONCluster(tt.spec)
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
				tt.check(t, c)
			}
		})
	}
}

func TestParseNativeHTTPFiltersBefore(t *testing.T) {
	validManifest, err := extensions.LoadLocalManifest("../extensions/testdata/native_http_filters_valid.yaml")
	require.NoError(t, err)

	orderedManifest := &extensions.Manifest{
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

	tests := []struct {
		name     string
		manifest *extensions.Manifest
		expect   string
	}{
		{
			name:     "valid",
			manifest: validManifest,
			expect: `[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
      "traffic_mode": "REJECT_NO_MCP",
      "request_storage_mode": "DYNAMIC_METADATA_AND_FILTER_STATE"
    }
  }
		]`,
		},
		{
			name:     "preserves order",
			manifest: orderedManifest,
			expect: `[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp"
    }
  },
  {
    "name": "envoy.filters.http.dynamic_modules",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter",
      "filter_name": "n",
      "dynamic_module_config": {
        "name": "n"
      }
    }
  }
]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNativeHTTPFiltersBefore(tt.manifest)
			require.NoError(t, err)
			requireProtoJSON(t, tt.expect, result)
		})
	}
}

func TestParseNativeHTTPFiltersBeforeErrors(t *testing.T) {
	unknownAtTypeManifest, err := extensions.LoadLocalManifest("testdata/input_native_unknown_at_type.yaml")
	require.NoError(t, err)

	malformedTypedConfigManifest, err := extensions.LoadLocalManifest("testdata/input_native_malformed_typed_config.yaml")
	require.NoError(t, err)

	terminalSnakeManifest, err := extensions.LoadLocalManifest("testdata/input_native_dynmod_terminal_snake.yaml")
	require.NoError(t, err)

	terminalCamelManifest, err := extensions.LoadLocalManifest("testdata/input_native_dynmod_terminal_camel.yaml")
	require.NoError(t, err)

	tests := []struct {
		name        string
		manifest    *extensions.Manifest
		expectedErr string
	}{
		{
			name: "marshal error",
			manifest: &extensions.Manifest{
				Name: "marshal-error",
				Type: extensions.TypeLua,
				NativeHTTPFilters: &extensions.NativeHTTPFilters{
					Before: []map[string]any{{
						"name": "envoy.filters.http.mcp",
						"typed_config": map[string]any{
							"@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
							"bad":   make(chan int),
						},
					}},
				},
			},
			expectedErr: "before[0]: marshal entry: json: unsupported type: chan int",
		},
		{
			name:        "unknown @type",
			manifest:    unknownAtTypeManifest,
			expectedErr: `before[0]: proto: (line 1:70): unable to resolve "type.googleapis.com/envoy.extensions.filters.http.example_unknown.v3.Unknown": "not found"`,
		},
		{
			name:        "malformed typed_config",
			manifest:    malformedTypedConfigManifest,
			expectedErr: `before[0]: proto: (line 1:136): invalid value for enum field trafficMode: "NOT_A_VALID_ENUM_VALUE"`,
		},
		{
			name:        "dynamic_modules terminal snake_case",
			manifest:    terminalSnakeManifest,
			expectedErr: "before[0]: envoy.filters.http.dynamic_modules with terminal_filter=true is not supported in nativeHttpFilters.before",
		},
		{
			name:        "dynamic_modules terminal camelCase",
			manifest:    terminalCamelManifest,
			expectedErr: "before[0]: envoy.filters.http.dynamic_modules with terminal_filter=true is not supported in nativeHttpFilters.before",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseNativeHTTPFiltersBefore(tt.manifest)
			requireEqualError(t, err, tt.expectedErr)
		})
	}
}

// TestRenderConfigGroupsNativeBeforePerExtension drives the composition end-to-end
// with two extensions, both declaring their own before[]. The HCM chain must
// emit each extension's before[] immediately before that extension's generated
// filter, and the same native filter must appear twice (once per declaring
// extension) — no cross-extension de-duplication.
func TestRenderConfigGroupsNativeBeforePerExtension(t *testing.T) {
	first, err := extensions.LoadLocalManifest("testdata/input_lua_inline.yaml")
	require.NoError(t, err)
	first.Name = "ext-one"
	first.NativeHTTPFilters = &extensions.NativeHTTPFilters{
		Before: []map[string]any{{
			"name": "envoy.filters.http.mcp",
			"typed_config": map[string]any{
				"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
				"traffic_mode": "REJECT_NO_MCP",
			},
		}},
	}

	second, err := extensions.LoadLocalManifest("testdata/input_lua_inline.yaml")
	require.NoError(t, err)
	second.Name = "ext-two"
	second.NativeHTTPFilters = &extensions.NativeHTTPFilters{
		Before: []map[string]any{{
			"name": "envoy.filters.http.mcp",
			"typed_config": map[string]any{
				"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
				"traffic_mode": "PASS_THROUGH",
			},
		}},
	}

	result, err := RenderConfig(&ConfigGenerationParams{
		Logger:       internaltesting.NewTLogger(t),
		AdminPort:    9901,
		ListenerPort: 10000,
		Extensions:   []*extensions.Manifest{first, second},
	}, MinimalConfigRenderer)
	require.NoError(t, err)
	require.YAMLEq(t, `http_filters:
- name: envoy.filters.http.mcp
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
    traffic_mode: REJECT_NO_MCP
- name: ext-one
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
    default_source_code:
      inline_string: |
        function envoy_on_request(request_handle)
          request_handle:logInfo("Hello, World!")
        end
- name: envoy.filters.http.mcp
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
- name: ext-two
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
    default_source_code:
      inline_string: |
        function envoy_on_request(request_handle)
          request_handle:logInfo("Hello, World!")
        end
`, result)
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
	validYAML := `- name: envoy.filters.http.mcp
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
    traffic_mode: PASS_THROUGH
`
	invalidYAML := "not a list"
	rejectTerminalDynamicModuleInput := `[
  {
    "name": "envoy.filters.http.dynamic_modules",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter",
      "filter_name": "n",
      "terminal_filter": true,
      "dynamic_module_config": { "name": "n" }
    }
  }
]`
	invalidYAMLSyntax := "- name: ["
	unknownTypeInput := `[
  {
    "name": "envoy.filters.http.example_unknown",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.example_unknown.v3.Unknown"
    }
  }
]`

	tests := []struct {
		name        string
		input       string
		expect      string
		expectedErr string
	}{
		{
			name:  "valid JSON list",
			input: mcpJSON,
			expect: `[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp"
    }
  }
]`,
		},
		{
			name:  "valid YAML list",
			input: validYAML,
			expect: `[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp"
    }
  }
]`,
		},
		{
			name:        "invalid YAML",
			input:       invalidYAML,
			expectedErr: "unmarshal filter list: json: cannot unmarshal string into Go value of type []json.RawMessage",
		},
		{
			name:        "rejects terminal dynamic module",
			input:       rejectTerminalDynamicModuleInput,
			expectedErr: "entry[0]: envoy.filters.http.dynamic_modules with terminal_filter=true is not supported in nativeHttpFilters.before",
		},
		{
			name:        "invalid YAML syntax",
			input:       invalidYAMLSyntax,
			expectedErr: "YAML to JSON: yaml: line 1: did not find expected node content",
		},
		{
			name:        "unknown type in override entry",
			input:       unknownTypeInput,
			expectedErr: `entry[0]: proto: (line 1:70): unable to resolve "type.googleapis.com/envoy.extensions.filters.http.example_unknown.v3.Unknown": "not found"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNativeHTTPFiltersBeforeOverride(tt.input)
			if tt.expectedErr != "" {
				requireEqualError(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
				requireProtoJSON(t, tt.expect, result)
			}
		})
	}

	t.Run("@filepath", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "filters.yaml")
		require.NoError(t, os.WriteFile(path, []byte(mcpJSON), 0o600))

		result, err := parseNativeHTTPFiltersBeforeOverride("@" + path)
		require.NoError(t, err)
		requireProtoJSON(t, `[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp"
    }
  }
]`, result)
	})

	t.Run("@filepath missing", func(t *testing.T) {
		path := "/definitely/missing/native-filters.yaml"
		_, err := parseNativeHTTPFiltersBeforeOverride("@" + path)
		requireEqualError(t, err, `read file "/definitely/missing/native-filters.yaml": open /definitely/missing/native-filters.yaml: no such file or directory`)
	})
}

func TestRejectTerminalDynamicModule(t *testing.T) {
	routerAny, err := anypb.New(&routerv3.Router{})
	require.NoError(t, err)

	nonTerminalAny, err := anypb.New(&dymhttpv3.DynamicModuleFilter{
		DynamicModuleConfig: &dymv3.DynamicModuleConfig{Name: "n"},
		FilterName:          "n",
	})
	require.NoError(t, err)

	terminalAny, err := anypb.New(&dymhttpv3.DynamicModuleFilter{
		DynamicModuleConfig: &dymv3.DynamicModuleConfig{Name: "n"},
		FilterName:          "n",
		TerminalFilter:      true,
	})
	require.NoError(t, err)

	dm := &dymhttpv3.DynamicModuleFilter{}
	err = routerAny.UnmarshalTo(dm)
	require.Error(t, err)
	wrongTypedConfigErr := fmt.Sprintf("unmarshal dynamic_modules typed_config: %v", err)

	tests := []struct {
		name        string
		filter      *hcmv3.HttpFilter
		expectedErr string
	}{
		{
			name:   "non dynamic_modules filter is ignored",
			filter: &hcmv3.HttpFilter{Name: "envoy.filters.http.router"},
		},
		{
			name:   "dynamic_modules without typed config is allowed",
			filter: &hcmv3.HttpFilter{Name: "envoy.filters.http.dynamic_modules"},
		},
		{
			name: "dynamic_modules with terminal_filter false is allowed",
			filter: &hcmv3.HttpFilter{
				Name:       "envoy.filters.http.dynamic_modules",
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: nonTerminalAny},
			},
		},
		{
			name: "dynamic_modules with terminal_filter true is rejected",
			filter: &hcmv3.HttpFilter{
				Name:       "envoy.filters.http.dynamic_modules",
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: terminalAny},
			},
			expectedErr: "envoy.filters.http.dynamic_modules with terminal_filter=true is not supported in nativeHttpFilters.before",
		},
		{
			name: "dynamic_modules with wrong typed config errors",
			filter: &hcmv3.HttpFilter{
				Name:       "envoy.filters.http.dynamic_modules",
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: routerAny},
			},
			expectedErr: wrongTypedConfigErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectTerminalDynamicModule(tt.filter)
			if tt.expectedErr != "" {
				requireEqualError(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRenderConfigWithNativeHTTPFiltersBeforeErrors(t *testing.T) {
	t.Run("override parse error is wrapped", func(t *testing.T) {
		ext, err := extensions.LoadLocalManifest("testdata/input_lua_inline.yaml")
		require.NoError(t, err)
		ext.Name = "override-error"
		override := "- name: ["
		_, err = yaml.YAMLToJSON([]byte(override))
		require.Error(t, err)
		expectedErr := fmt.Sprintf("failed to generate config resources: failed to parse --native-http-filter-before for extension %q: YAML to JSON: %v",
			ext.Name, err)

		_, err = RenderConfig(&ConfigGenerationParams{
			Logger:                  internaltesting.NewTLogger(t),
			AdminPort:               9901,
			ListenerPort:            10000,
			Extensions:              []*extensions.Manifest{ext},
			NativeHTTPFiltersBefore: []string{override},
		}, MinimalConfigRenderer)
		requireEqualError(t, err, expectedErr)
	})

	t.Run("manifest parse error is wrapped", func(t *testing.T) {
		ext, err := extensions.LoadLocalManifest("testdata/input_native_unknown_at_type.yaml")
		require.NoError(t, err)
		raw, err := json.Marshal(ext.NativeHTTPFilters.Before[0])
		require.NoError(t, err)

		filter := &hcmv3.HttpFilter{}
		err = protojson.Unmarshal(raw, filter)
		require.Error(t, err)
		expectedErr := fmt.Sprintf("failed to generate config resources: failed to parse nativeHttpFilters.before for extension %q: before[0]: %v",
			ext.Name, err)

		_, err = RenderConfig(&ConfigGenerationParams{
			Logger:       internaltesting.NewTLogger(t),
			AdminPort:    9901,
			ListenerPort: 10000,
			Extensions:   []*extensions.Manifest{ext},
		}, MinimalConfigRenderer)
		requireEqualError(t, err, expectedErr)
	})
}

func TestRenderConfigWithNativeHTTPFiltersBeforeOverride(t *testing.T) {
	tests := []struct {
		name                     string
		manifestNativeHTTPBefore *extensions.NativeHTTPFilters
		nativeHTTPFiltersBefore  []string
		expect                   string
	}{
		{
			name: "override replaces manifest before",
			manifestNativeHTTPBefore: &extensions.NativeHTTPFilters{
				Before: []map[string]any{{
					"name": "envoy.filters.http.mcp",
					"typed_config": map[string]any{
						"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
						"traffic_mode": "REJECT_NO_MCP",
					},
				}},
			},
			nativeHTTPFiltersBefore: []string{`[
  {
    "name": "envoy.filters.http.mcp",
    "typed_config": {
      "@type": "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
      "traffic_mode": "PASS_THROUGH"
    }
  }
]`},
			expect: `http_filters:
- name: envoy.filters.http.mcp
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
- name: ext-override-test
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
    default_source_code:
      inline_string: |
        function envoy_on_request(request_handle)
          request_handle:logInfo("Hello, World!")
        end
`,
		},
		{
			name: "empty override falls back to manifest",
			manifestNativeHTTPBefore: &extensions.NativeHTTPFilters{
				Before: []map[string]any{{
					"name": "envoy.filters.http.mcp",
					"typed_config": map[string]any{
						"@type":        "type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp",
						"traffic_mode": "REJECT_NO_MCP",
					},
				}},
			},
			nativeHTTPFiltersBefore: []string{""},
			expect: `http_filters:
- name: envoy.filters.http.mcp
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.mcp.v3.Mcp
    traffic_mode: REJECT_NO_MCP
- name: ext-override-test
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
    default_source_code:
      inline_string: |
        function envoy_on_request(request_handle)
          request_handle:logInfo("Hello, World!")
        end
`,
		},
		{
			name:                     "no manifest before and no override",
			manifestNativeHTTPBefore: nil,
			nativeHTTPFiltersBefore:  nil,
			expect: `http_filters:
- name: ext-override-test
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
    default_source_code:
      inline_string: |
        function envoy_on_request(request_handle)
          request_handle:logInfo("Hello, World!")
        end
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, err := extensions.LoadLocalManifest("testdata/input_lua_inline.yaml")
			require.NoError(t, err)
			ext.Name = "ext-override-test"
			ext.NativeHTTPFilters = tt.manifestNativeHTTPBefore

			result, err := RenderConfig(&ConfigGenerationParams{
				Logger:                  internaltesting.NewTLogger(t),
				AdminPort:               9901,
				ListenerPort:            10000,
				Extensions:              []*extensions.Manifest{ext},
				NativeHTTPFiltersBefore: tt.nativeHTTPFiltersBefore,
			}, MinimalConfigRenderer)
			require.NoError(t, err)
			require.YAMLEq(t, tt.expect, result)
		})
	}
}

func requireEqualError(t *testing.T, err error, expected string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, expected, strings.ReplaceAll(err.Error(), "\u00a0", " "))
}

func requireProtoJSON(t *testing.T, expect string, result []*hcmv3.HttpFilter) {
	t.Helper()

	actual, err := protoListToAny(result)
	require.NoError(t, err)

	data, err := json.Marshal(actual)
	require.NoError(t, err)

	require.JSONEq(t, expect, string(data))
}
