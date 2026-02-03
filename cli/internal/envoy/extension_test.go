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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestGenerateFilterConfigUnsupportedType(t *testing.T) {
	manifest := extensions.Manifest{Type: "unsupported_type"}
	_, err := GenerateFilterConfig(&manifest, "", nil)
	require.ErrorIs(t, err, ErrUnsupportedExtensionType)
}

func TestGenerateFilterConfigUnimplemented(t *testing.T) {
	for _, et := range []extensions.Type{
		extensions.TypeWasm,
	} {
		t.Run(string(et), func(t *testing.T) {
			manifest := extensions.Manifest{Type: et}
			_, err := GenerateFilterConfig(&manifest, "", nil)
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

			got, err := GenerateFilterConfig(localManifest, "", nil)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}

			checkProtos(t, tt.want.HTTPFilters, got.HTTPFilters)
		})
	}
}

func TestDynamicModuleFilterGenerator(t *testing.T) {
	dataHome := t.TempDir()
	manifest := &extensions.Manifest{
		Name:    "test-dynamic-module",
		Type:    extensions.TypeDynamicModule,
		Version: "v1.0.0",
	}

	// Case 1: Composer binary missing
	_, err := GenerateFilterConfig(manifest, dataHome, nil)
	require.ErrorContains(t, err, "composer binary not found")

	// Case 2: Composer binary exists
	composerPath := filepath.Join(dataHome, "extensions", "dym", "composer", manifest.Version)
	require.NoError(t, os.MkdirAll(composerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(composerPath, "libcomposer.so"), []byte("fake binary"), 0o600))

	got, err := GenerateFilterConfig(manifest, dataHome, nil)
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
							FilterName: manifest.Name,
						}
						cfg, err := anypb.New(dymConfig)
						require.NoError(t, err)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.HTTPFilters, got.HTTPFilters)
}

func TestComposerFilterGenerator(t *testing.T) {
	dataHome := t.TempDir()
	manifest := &extensions.Manifest{
		Name:            "test-composer",
		Type:            extensions.TypeComposer,
		Version:         "v0.0.1",
		ComposerVersion: "v1.0.0",
	}

	// Case 1: Composer binary missing
	_, err := GenerateFilterConfig(manifest, dataHome, nil)
	require.ErrorContains(t, err, "composer binary not found")

	// Create Composer binary
	composerPath := filepath.Join(dataHome, "extensions", "dym", "composer", manifest.ComposerVersion)
	require.NoError(t, os.MkdirAll(composerPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(composerPath, "libcomposer.so"), []byte("fake binary"), 0o600))

	// Case 2: Plugin binary missing
	_, err = GenerateFilterConfig(manifest, dataHome, nil)
	require.ErrorContains(t, err, "go plugin binary not found")

	// Create Plugin binary
	pluginPath := filepath.Join(dataHome, "extensions", "goplugin", manifest.Name, manifest.Version)
	require.NoError(t, os.MkdirAll(pluginPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(pluginPath, "plugin.so"), []byte("fake binary"), 0o600))

	// Case 3: Success
	got, err := GenerateFilterConfig(manifest, dataHome, nil)
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
								value := fmt.Sprintf(`{"name":"test-composer", "url":"file://%s/extensions/goplugin/test-composer/v0.0.1/plugin.so"}`, dataHome)
								cfg, err := anypb.New(wrapperspb.String(value))
								require.NoError(t, err)
								return cfg
							}(),
						}
						cfg, err := anypb.New(dymConfig)
						require.NoError(t, err)
						return cfg
					}(),
				},
			},
		},
	}

	checkProtos(t, want.HTTPFilters, got.HTTPFilters)
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
