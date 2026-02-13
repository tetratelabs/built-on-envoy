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

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	dymv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/dynamic_modules/v3"
	dymhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_modules/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestGenerateFilterConfigUnsupportedType(t *testing.T) {
	manifest := extensions.Manifest{Type: "unsupported_type"}
	_, err := GenerateFilterConfig(&manifest, &xdg.Directories{}, "")
	require.ErrorIs(t, err, ErrUnsupportedExtensionType)
}

func TestGenerateFilterConfigUnimplemented(t *testing.T) {
	for _, et := range []extensions.Type{
		extensions.TypeWasm,
	} {
		t.Run(string(et), func(t *testing.T) {
			manifest := extensions.Manifest{Type: et}
			_, err := GenerateFilterConfig(&manifest, &xdg.Directories{}, "")
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

			got, err := GenerateFilterConfig(localManifest, &xdg.Directories{}, "")
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}

			checkProtos(t, tt.want.HTTPFilters, got.HTTPFilters)
		})
	}
}

func TestDynamicModuleFilterGenerator(t *testing.T) {
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:    "test-dynamic-module",
		Type:    extensions.TypeDynamicModule,
		Version: "v1.0.0",
		Remote:  true,
	}

	// Case 1: Generate config for Rust dynamic module
	got, err := GenerateFilterConfig(manifest, dirs, "")
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

	// Case 2: Success with config
	configJSON := `{"key":"value","nested":{"foo":"bar"}}`
	got, err = GenerateFilterConfig(manifest, dirs, configJSON)
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

func TestComposerFilterGenerator(t *testing.T) {
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	manifest := &extensions.Manifest{
		Name:            "test-composer",
		Type:            extensions.TypeComposer,
		Version:         "v0.0.1",
		ComposerVersion: "v1.0.0",
		Remote:          true,
	}

	// Case 1: Composer binary missing
	_, err := GenerateFilterConfig(manifest, dirs, "")
	require.ErrorContains(t, err, "composer binary not found")

	// Create Composer binary
	composerPath := extensions.LocalCacheComposerDir(dirs, manifest.ComposerVersion, !manifest.Remote)
	require.NoError(t, os.MkdirAll(composerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(composerPath, "libcomposer.so"), []byte("fake binary"), 0o600))

	// Case 2: Plugin binary missing
	_, err = GenerateFilterConfig(manifest, dirs, "")
	require.ErrorContains(t, err, "go plugin binary not found")

	// Create Plugin binary
	pluginPath := filepath.Join(dirs.DataHome, "extensions", "goplugin", manifest.Name, manifest.Version)
	require.NoError(t, os.MkdirAll(pluginPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(pluginPath, "plugin.so"), []byte("fake binary"), 0o600))

	// Case 3: Success without config
	got, err := GenerateFilterConfig(manifest, dirs, "")
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
							FilterName: "goplugin",
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
	got, err = GenerateFilterConfig(manifest, dirs, configJSON)
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
							FilterName: "goplugin",
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
