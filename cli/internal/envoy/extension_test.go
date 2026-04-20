// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	dymv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/dynamic_modules/v3"
	dymhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_modules/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	dymlistv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/dynamic_modules/v3"
	dymnetv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/dynamic_modules/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	dymudpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/dynamic_modules/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestGenerateFilterConfigUnsupportedType(t *testing.T) {
	manifest := extensions.Manifest{Type: "unsupported_type"}
	_, err := GenerateFilterConfig(internaltesting.NewTLogger(t), &manifest, &xdg.Directories{}, "")
	require.ErrorIs(t, err, ErrUnsupportedExtensionType)
}

func TestGenerateFilterConfigUnimplemented(t *testing.T) {
	for _, et := range []extensions.Type{
		extensions.TypeWasm,
	} {
		t.Run(string(et), func(t *testing.T) {
			manifest := extensions.Manifest{Type: et}
			_, err := GenerateFilterConfig(internaltesting.NewTLogger(t), &manifest, &xdg.Directories{}, "")
			require.ErrorIs(t, err, ErrUnimplemented)
		})
	}
}

func TestLuaGenerateFilterConfig(t *testing.T) {
	tests := []struct {
		name    string
		want    *ExtensionResources
		wantErr error
	}{
		{name: "lua_invalid_path.yaml", wantErr: ErrLuaLoadFile},
		{
			name: "lua_inline.yaml",
			want: &ExtensionResources{
				HTTPFilters: []*hcmv3.HttpFilter{
					{
						Name: "test-extension",
						ConfigType: &hcmv3.HttpFilter_TypedConfig{
							TypedConfig: func() *anypb.Any {
								luaConfig := &luav3.Lua{
									DefaultSourceCode: &corev3.DataSource{
										Specifier: &corev3.DataSource_InlineString{
											InlineString: `function envoy_on_request(request_handle)
  request_handle:logInfo("Hello, World!")
end
`,
										},
									},
								}
								cfg, err := anypb.New(luaConfig)
								require.NoError(t, err)
								return cfg
							}(),
						},
					},
				},
			},
		},
		{
			name: "lua_path.yaml",
			want: &ExtensionResources{
				HTTPFilters: []*hcmv3.HttpFilter{
					{
						Name: "test-extension",
						ConfigType: &hcmv3.HttpFilter_TypedConfig{
							TypedConfig: func() *anypb.Any {
								luaConfig := &luav3.Lua{
									DefaultSourceCode: &corev3.DataSource{
										Specifier: &corev3.DataSource_InlineString{
											InlineString: `-- Copyright Built On Envoy
-- SPDX-License-Identifier: Apache-2.0
-- The full text of the Apache license is available in the LICENSE file at
-- the root of the repo.

function envoy_on_request(request_handle)
  request_handle:logInfo("Hello, World!")
end
`,
										},
									},
								}
								cfg, err := anypb.New(luaConfig)
								require.NoError(t, err)
								return cfg
							}(),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestPath := filepath.Join("testdata", "input_"+tt.name)
			localManifest, err := extensions.LoadLocalManifest(manifestPath)
			require.NoError(t, err)

			got, err := GenerateFilterConfig(internaltesting.NewTLogger(t), localManifest, &xdg.Directories{}, "")
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}

			checkProtos(t, tt.want.HTTPFilters, got.HTTPFilters)
		})
	}
}

func TestDynamicModuleFilterGeneratorHTTP(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:    "test-dynamic-module",
		Type:    extensions.TypeRust,
		Version: "v1.0.0",
		Remote:  true,
	}

	// Case 1: Generate config for HTTP Filter written as Rust dynamic module
	got, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)

	want := &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymhttpv3.DynamicModuleFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.HTTPFilters, got.HTTPFilters)

	// Case 2: Success with config for HTTP Filter written as Rust dynamic module
	configJSON := `{"key":"value","nested":{"foo":"bar"}}`
	got, err = GenerateFilterConfig(logger, manifest, dirs, configJSON)
	require.NoError(t, err, "GenerateFilterConfig with config failed")

	wantWithConfig := &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymhttpv3.DynamicModuleFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
							FilterConfig: func() *anypb.Any {
								cfg, err := anypb.New(wrapperspb.String(configJSON))
								require.NoError(t, err, "marshal StringValue to Any failed")
								return cfg
							}(),
						}
						cfg, err := anypb.New(dymConfig)
						require.NoError(t, err, "marshal DynamicModuleFilter to Any failed")
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, wantWithConfig.HTTPFilters, got.HTTPFilters)
}

func TestDynamicModuleFilterGeneratorNetwork(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:       "test-network-module",
		Type:       extensions.TypeRust,
		FilterType: extensions.FilterTypeNetwork,
		Version:    "v1.0.0",
		Remote:     true,
	}

	// Case 1: Generate config for Network Filter written as Rust dynamic module
	got, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)
	require.Empty(t, got.HTTPFilters, "network filter should not produce HTTP filters")

	want := &ExtensionResources{
		NetworkFilters: []*listenerv3.Filter{
			{
				Name: manifest.Name,
				ConfigType: &listenerv3.Filter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymnetv3.DynamicModuleNetworkFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.NetworkFilters, got.NetworkFilters)

	// Case 2: Success with config for Network Filter written as Rust dynamic module
	configJSON := `{"key":"value","nested":{"foo":"bar"}}`
	got, err = GenerateFilterConfig(logger, manifest, dirs, configJSON)
	require.NoError(t, err, "GenerateFilterConfig with config failed")
	require.Empty(t, got.HTTPFilters, "network filter should not produce HTTP filters")

	wantWithConfig := &ExtensionResources{
		NetworkFilters: []*listenerv3.Filter{
			{
				Name: manifest.Name,
				ConfigType: &listenerv3.Filter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymnetv3.DynamicModuleNetworkFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
							FilterConfig: func() *anypb.Any {
								cfg, err := anypb.New(wrapperspb.String(configJSON))
								require.NoError(t, err, "marshal StringValue to Any failed")
								return cfg
							}(),
						}
						cfg, err := anypb.New(dymConfig)
						require.NoError(t, err, "marshal DynamicModuleNetworkFilter to Any failed")
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, wantWithConfig.NetworkFilters, got.NetworkFilters)
}

func TestDynamicModuleFilterGeneratorListener(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:       "test-listener-module",
		Type:       extensions.TypeRust,
		FilterType: extensions.FilterTypeListener,
		Version:    "v1.0.0",
		Remote:     true,
	}

	// Case 1: Generate config for Listener Filter written as Rust dynamic module
	got, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)
	require.Empty(t, got.HTTPFilters, "listener filter should not produce HTTP filters")
	require.Empty(t, got.NetworkFilters, "listener filter should not produce network filters")

	want := &ExtensionResources{
		ListenerFilters: []*listenerv3.ListenerFilter{
			{
				Name: manifest.Name,
				ConfigType: &listenerv3.ListenerFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymlistv3.DynamicModuleListenerFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.ListenerFilters, got.ListenerFilters)

	// Case 2: Success with config for Listener Filter written as Rust dynamic module
	configJSON := `{"key":"value","nested":{"foo":"bar"}}`
	got, err = GenerateFilterConfig(logger, manifest, dirs, configJSON)
	require.NoError(t, err, "GenerateFilterConfig with config failed")
	require.Empty(t, got.HTTPFilters, "listener filter should not produce HTTP filters")
	require.Empty(t, got.NetworkFilters, "listener filter should not produce network filters")

	wantWithConfig := &ExtensionResources{
		ListenerFilters: []*listenerv3.ListenerFilter{
			{
				Name: manifest.Name,
				ConfigType: &listenerv3.ListenerFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymlistv3.DynamicModuleListenerFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
							FilterConfig: func() *anypb.Any {
								cfg, err := anypb.New(wrapperspb.String(configJSON))
								require.NoError(t, err, "marshal StringValue to Any failed")
								return cfg
							}(),
						}
						cfg, err := anypb.New(dymConfig)
						require.NoError(t, err, "marshal DynamicModuleListenerFilter to Any failed")
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, wantWithConfig.ListenerFilters, got.ListenerFilters)
}

func TestDynamicModuleFilterGeneratorUDPListener(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:       "test-udp-listener-module",
		Type:       extensions.TypeRust,
		FilterType: extensions.FilterTypeUDPListener,
		Version:    "v1.0.0",
		Remote:     true,
	}

	// Case 1: Generate config for UDP Listener Filter written as Rust dynamic module
	got, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)
	require.Empty(t, got.HTTPFilters, "UDP listener filter should not produce HTTP filters")
	require.Empty(t, got.NetworkFilters, "UDP listener filter should not produce network filters")

	want := &ExtensionResources{
		ListenerFilters: []*listenerv3.ListenerFilter{
			{
				Name: manifest.Name,
				ConfigType: &listenerv3.ListenerFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymudpv3.DynamicModuleUdpListenerFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.ListenerFilters, got.ListenerFilters)

	// Case 2: Success with config for UDP Listener Filter written as Rust dynamic module
	configJSON := `{"key":"value","nested":{"foo":"bar"}}`
	got, err = GenerateFilterConfig(logger, manifest, dirs, configJSON)
	require.NoError(t, err, "GenerateFilterConfig with config failed")
	require.Empty(t, got.HTTPFilters, "UDP listener filter should not produce HTTP filters")
	require.Empty(t, got.NetworkFilters, "UDP listener filter should not produce network filters")

	wantWithConfig := &ExtensionResources{
		ListenerFilters: []*listenerv3.ListenerFilter{
			{
				Name: manifest.Name,
				ConfigType: &listenerv3.ListenerFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymudpv3.DynamicModuleUdpListenerFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         manifest.Name,
								LoadGlobally: false,
							},
							FilterName: manifest.Name,
							FilterConfig: func() *anypb.Any {
								cfg, err := anypb.New(wrapperspb.String(configJSON))
								require.NoError(t, err, "marshal StringValue to Any failed")
								return cfg
							}(),
						}
						cfg, err := anypb.New(dymConfig)
						require.NoError(t, err, "marshal DynamicModuleUdpListenerFilter to Any failed")
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, wantWithConfig.ListenerFilters, got.ListenerFilters)
}

func TestComposerFilterGenerator(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:            "test-composer",
		Type:            extensions.TypeGo,
		Version:         "v0.0.1",
		ComposerVersion: "v1.0.0",
		Remote:          true,
	}

	// Case 1: Composer binary missing
	_, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.ErrorContains(t, err, "composer binary not found")

	// Create Composer binary
	composerPath := extensions.LocalCacheComposerDir(dirs, manifest.ComposerVersion)
	require.NoError(t, os.MkdirAll(composerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(composerPath, "libcomposer.so"), []byte("fake binary"), 0o600))

	// Case 2: Plugin binary missing
	_, err = GenerateFilterConfig(logger, manifest, dirs, "")
	require.ErrorContains(t, err, "go plugin binary not found")

	// Create Plugin binary
	pluginPath := filepath.Join(dirs.DataHome, "extensions", "goplugin", manifest.Name, manifest.Version)
	require.NoError(t, os.MkdirAll(pluginPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(pluginPath, "plugin.so"), []byte("fake binary"), 0o600))

	// Case 3: Success without config
	got, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)

	want := &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymhttpv3.DynamicModuleFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         "composer",
								LoadGlobally: true,
							},
							FilterName: "goplugin-loader",
							FilterConfig: func() *anypb.Any {
								configStruct := &structpb.Struct{
									Fields: map[string]*structpb.Value{
										"name":         structpb.NewStringValue(manifest.Name),
										"url":          structpb.NewStringValue(fmt.Sprintf("file://%s/extensions/goplugin/%s/%s/plugin.so", dirs.DataHome, manifest.Name, manifest.Version)),
										"config":       structpb.NewNullValue(),
										"strict_check": structpb.NewBoolValue(false),
									},
								}
								marshaledJSON, marshalErr := protojson.Marshal(configStruct)
								require.NoError(t, marshalErr)
								cfg, anypbErr := anypb.New(wrapperspb.String(string(marshaledJSON)))
								require.NoError(t, anypbErr)
								return cfg
							}(),
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.HTTPFilters, got.HTTPFilters)

	// Case 4: Success with config
	configJSON := `{"key":"value","nested":{"foo":"bar"}}`
	got, err = GenerateFilterConfig(logger, manifest, dirs, configJSON)
	require.NoError(t, err, "GenerateFilterConfig with config failed")

	wantWithConfig := &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						innerStruct := &structpb.Struct{}
						unmarshalErr := protojson.Unmarshal([]byte(configJSON), innerStruct)
						require.NoError(t, unmarshalErr)

						dymConfig := &dymhttpv3.DynamicModuleFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         "composer",
								LoadGlobally: true,
							},
							FilterName: "goplugin-loader",
							FilterConfig: func() *anypb.Any {
								configStruct := &structpb.Struct{
									Fields: map[string]*structpb.Value{
										"name":         structpb.NewStringValue(manifest.Name),
										"url":          structpb.NewStringValue(fmt.Sprintf("file://%s/extensions/goplugin/%s/%s/plugin.so", dirs.DataHome, manifest.Name, manifest.Version)),
										"config":       structpb.NewStructValue(innerStruct),
										"strict_check": structpb.NewBoolValue(false),
									},
								}
								marshaledJSON, marshalErr := protojson.Marshal(configStruct)
								require.NoError(t, marshalErr, "marshal config struct to JSON failed")
								cfg, anypbErr := anypb.New(wrapperspb.String(string(marshaledJSON)))
								require.NoError(t, anypbErr, "marshal StringValue to Any failed")
								return cfg
							}(),
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr, "marshal DynamicModuleFilter to Any failed")
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, wantWithConfig.HTTPFilters, got.HTTPFilters)

	// Case 5: Remote extension with SourceRegistry generates oci:// URL
	ociManifest := &extensions.Manifest{
		Name:            "test-composer",
		Type:            extensions.TypeGo,
		Version:         "v0.0.1",
		ComposerVersion: "v1.0.0",
		Remote:          true,
		SourceRegistry:  "ghcr.io/tetratelabs/built-on-envoy",
		SourceTag:       "0.0.1",
	}

	// Composer binary is already created from earlier in the test
	got, err = GenerateFilterConfig(logger, ociManifest, dirs, "")
	require.NoError(t, err)

	wantOCI := &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: ociManifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: func() *anypb.Any {
						dymConfig := &dymhttpv3.DynamicModuleFilter{
							DynamicModuleConfig: &dymv3.DynamicModuleConfig{
								Name:         "composer",
								LoadGlobally: true,
							},
							FilterName: "goplugin-loader",
							FilterConfig: func() *anypb.Any {
								configStruct := &structpb.Struct{
									Fields: map[string]*structpb.Value{
										"name":         structpb.NewStringValue(ociManifest.Name),
										"url":          structpb.NewStringValue("oci://ghcr.io/tetratelabs/built-on-envoy/extension-test-composer:0.0.1"),
										"config":       structpb.NewNullValue(),
										"strict_check": structpb.NewBoolValue(false),
									},
								}
								marshaledJSON, marshalErr := protojson.Marshal(configStruct)
								require.NoError(t, marshalErr)
								cfg, anypbErr := anypb.New(wrapperspb.String(string(marshaledJSON)))
								require.NoError(t, anypbErr)
								return cfg
							}(),
						}
						cfg, anypbErr := anypb.New(dymConfig)
						require.NoError(t, anypbErr)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, wantOCI.HTTPFilters, got.HTTPFilters)
}

func TestExtProcFilterGenerator(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	dirs := &xdg.Directories{DataHome: t.TempDir()}

	// Case 1: Invalid message timeout
	manifest := &extensions.Manifest{
		Name:    "test-ext-proc",
		Type:    extensions.TypeExtProc,
		Version: "v0.0.1",
		ExtProc: &extensions.ExtProc{
			GRPCPort:       50051,
			MessageTimeout: "not-a-duration",
		},
	}
	_, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.ErrorContains(t, err, "invalid messageTimeout")

	// Case 2: Minimal config (no processing mode, no timeout)
	manifest = &extensions.Manifest{
		Name:    "test-ext-proc",
		Type:    extensions.TypeExtProc,
		Version: "v0.0.1",
		ExtProc: &extensions.ExtProc{
			GRPCPort: 50051,
		},
	}
	got, err := GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)

	clusterName := manifest.Name + extProcClusterSuffix
	wantMinimal := buildExtProcResources(t, manifest.Name, clusterName, 50051, false, nil, 0)
	checkProtos(t, wantMinimal.HTTPFilters, got.HTTPFilters)
	checkProtos(t, wantMinimal.Clusters, got.Clusters)

	// Case 3: Full config (processing mode + message timeout + failureModeAllow)
	manifest = &extensions.Manifest{
		Name:    "test-ext-proc-full",
		Type:    extensions.TypeExtProc,
		Version: "v0.0.1",
		ExtProc: &extensions.ExtProc{
			GRPCPort:         50052,
			FailureModeAllow: true,
			MessageTimeout:   "500ms",
			ProcessingMode: &extensions.ExtProcProcessingMode{
				RequestHeaderMode:  "SEND",
				ResponseHeaderMode: "SKIP",
				RequestBodyMode:    "BUFFERED",
				ResponseBodyMode:   "NONE",
			},
		},
	}
	got, err = GenerateFilterConfig(logger, manifest, dirs, "")
	require.NoError(t, err)

	clusterName = manifest.Name + extProcClusterSuffix
	processingMode := &extprocv3.ProcessingMode{
		RequestHeaderMode:  extprocv3.ProcessingMode_SEND,
		ResponseHeaderMode: extprocv3.ProcessingMode_SKIP,
		RequestBodyMode:    extprocv3.ProcessingMode_BUFFERED,
		ResponseBodyMode:   extprocv3.ProcessingMode_NONE,
	}
	wantFull := buildExtProcResources(t, manifest.Name, clusterName, 50052, true, processingMode, 500*time.Millisecond)
	checkProtos(t, wantFull.HTTPFilters, got.HTTPFilters)
	checkProtos(t, wantFull.Clusters, got.Clusters)
}

// buildExtProcResources constructs the expected ExtensionResources for an ext_proc extension.
func buildExtProcResources(t *testing.T, name, clusterName string, port int, failureModeAllow bool, processingMode *extprocv3.ProcessingMode, timeout time.Duration) *ExtensionResources {
	t.Helper()

	extProcFilter := &extprocv3.ExternalProcessor{
		GrpcService: &corev3.GrpcService{
			TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
					ClusterName: clusterName,
				},
			},
		},
		FailureModeAllow: failureModeAllow,
		ProcessingMode:   processingMode,
	}
	if timeout > 0 {
		extProcFilter.MessageTimeout = durationpb.New(timeout)
	}
	filterAny, err := anypb.New(extProcFilter)
	require.NoError(t, err)

	httpProtocolOptions := &httpv3.HttpProtocolOptions{
		UpstreamProtocolOptions: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_{
			ExplicitHttpConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
				},
			},
		},
	}
	httpProtocolOptionsAny, err := anypb.New(httpProtocolOptions)
	require.NoError(t, err)

	cluster := &clusterv3.Cluster{
		Name: clusterName,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_STATIC,
		},
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": httpProtocolOptionsAny,
		},
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: &corev3.Address{
										Address: &corev3.Address_SocketAddress{
											SocketAddress: &corev3.SocketAddress{
												Address: "127.0.0.1",
												PortSpecifier: &corev3.SocketAddress_PortValue{
													PortValue: uint32(port), //nolint:gosec
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name:       name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: filterAny},
			},
		},
		Clusters: []*clusterv3.Cluster{cluster},
	}
}

// checkProtosList checks if two lists of proto messages are equal and if not, prints their YAML representation
// for easier debugging.
func checkProtos[T proto.Message](t *testing.T, want, got []T) {
	require.Len(t, got, len(want))
	for i := range got {
		checkProto(t, want[i], got[i])
	}
}

// checkProto checks if two proto messages are equal and if not, prints their YAML representation
// for easier debugging.
func checkProto[T proto.Message](t *testing.T, want, got T) {
	if !proto.Equal(want, got) {
		wantYaml, err := ProtoToYaml(want)
		require.NoError(t, err)
		gotYaml, err := ProtoToYaml(got)
		require.NoError(t, err)
		require.YAMLEq(t,
			string(wantYaml), string(gotYaml),
			"want:\n%s\ngot:\n%s", wantYaml, gotYaml)
	}
}
