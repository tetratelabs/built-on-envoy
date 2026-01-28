// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"fmt"
	"os"
	"path"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	dym "github.com/envoyproxy/go-control-plane/envoy/extensions/dynamic_modules/v3"
	dymhttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_modules/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

type (
	// ExtensionFilterGenerator defines an interface for generating filter configurations
	ExtensionFilterGenerator interface {
		// GenerateFilterConfig generates the filter configuration for the given extension manifest.
		GenerateFilterConfig(manifest *extensions.Manifest, config any) (*ExtensionResources, error)
	}

	// LuaFilterGenerator generates filter configuration for Lua extensions.
	LuaFilterGenerator struct{}
	// WasmFilterGenerator generates filter configuration for Wasm extensions.
	WasmFilterGenerator struct{}
	// DynamicModuleFilterGenerator generates filter configuration for Dynamic Module extensions.
	DynamicModuleFilterGenerator struct{}
	// ComposerFilterGenerator generates filter configuration for Composer extensions.
	ComposerFilterGenerator struct{}

	// ExtensionResources holds the resources created by an extension.
	ExtensionResources struct {
		HTTPFilters []*hcmv3.HttpFilter
		Clusters    []*clusterv3.Cluster
		// TODO(huabing): may need to add more resources
	}
)

var (
	// ErrUnsupportedExtensionType is returned when an extension type is not supported.
	ErrUnsupportedExtensionType = fmt.Errorf("unsupported extension type")
	// ErrUnimplemented is returned when an extension filter generation is not yet implemented.
	ErrUnimplemented = fmt.Errorf("extension filter generation not yet implemented")
	// ErrLuaLoadFile is returned when loading Lua code from file fails.
	ErrLuaLoadFile = fmt.Errorf("failed to load Lua file")
)

// GenerateFilterConfig generates the filter configuration for the given extension manifest.
func GenerateFilterConfig(manifest *extensions.Manifest, config any) (*ExtensionResources, error) {
	var generator ExtensionFilterGenerator

	switch manifest.Type {
	case extensions.TypeLua:
		generator = LuaFilterGenerator{}
	case extensions.TypeWasm:
		generator = WasmFilterGenerator{}
	case extensions.TypeDynamicModule:
		generator = DynamicModuleFilterGenerator{}
	case extensions.TypeComposer:
		generator = ComposerFilterGenerator{}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedExtensionType, manifest.Type)
	}

	return generator.GenerateFilterConfig(manifest, config)
}

// GenerateFilterConfig generates the filter configuration for Lua extensions.
func (l LuaFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, _ any) (*ExtensionResources, error) {
	var code string
	if manifest.Lua.Path != "" {
		absPath := path.Join(path.Dir(manifest.Path), manifest.Lua.Path)
		bytes, err := os.ReadFile(path.Clean(absPath))
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", ErrLuaLoadFile, manifest.Lua.Path, err)
		}
		code = string(bytes)
	} else if manifest.Lua.Inline != "" {
		code = manifest.Lua.Inline
	}

	luaFilter := &luav3.Lua{
		DefaultSourceCode: &corev3.DataSource{
			Specifier: &corev3.DataSource_InlineString{
				InlineString: code,
			},
		},
	}
	luaAny, err := anypb.New(luaFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Lua filter to Any: %w", err)
	}

	filter := &hcmv3.HttpFilter{
		Name: manifest.Name,
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: luaAny,
		},
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{filter},
	}, nil
}

// GenerateFilterConfig generates the filter configuration for Wasm extensions.
func (w WasmFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*ExtensionResources, error) {
	return nil, fmt.Errorf("%w: wasm", ErrUnimplemented)
}

// GenerateFilterConfig generates the filter configuration for Dynamic Module extensions.
func (d DynamicModuleFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest,
	_ any) (*ExtensionResources, error) {

	// TODO(wbpcode): For now, we only support Composer dynamic modules because all golang dynamic
	// modules will be compiled into the same binary.
	// Once we support other dynamic modules, we need to differentiate them here.
	cachedComposerPath := getComposerPath(manifest.Version)
	if _, err := os.Stat(cachedComposerPath); os.IsNotExist(err) {
		// TODO(wbpcode): Download the composer binary from the URL specified in the manifest.
		return nil, fmt.Errorf("composer binary not found at %s", cachedComposerPath)
	}

	protoConfig := &dymhttp.DynamicModuleFilter{
		DynamicModuleConfig: &dym.DynamicModuleConfig{
			Name:         "composer",
			LoadGlobally: true,
		},
		FilterName:   manifest.Name,
		FilterConfig: nil, // TODO(wbpcode): Support passing filter config to composer extensions.
	}
	composerAny, err := anypb.New(protoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dynamic module filter to Any: %w", err)
	}

	return &hcmv3.HttpFilter{
		Name: manifest.Name,
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: composerAny,
		},
	}, nil
}

func getComposerPath(composerVersion string) string {
	// Get home directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// Build the path: $HOME/.built-on-envoy/cache/composer/v$version/libcomposer.so
	return fmt.Sprintf("%s/.built-on-envoy/cache/dym/composer/v%s/libcomposer.so", home, composerVersion)
}

func getGoPluginPathFromManifest(manifest *extensions.Manifest) string {
	// Get home directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// Build the path: $HOME/.built-on-envoy/cache/goplugin/$name/v$version/plugin.so
	return fmt.Sprintf("%s/.built-on-envoy/cache/goplugin/%s/v%s/plugin.so", home, manifest.Name, manifest.Version)
}

// GenerateFilterConfig generates the filter configuration for Composer extensions.
func (c ComposerFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest,
	_ any) (*hcmv3.HttpFilter, error) {

	cachedComposerPath := getComposerPath(manifest.ComposerVersion)
	if _, err := os.Stat(cachedComposerPath); os.IsNotExist(err) {
		// TODO(wbpcode): Download the composer binary from the URL specified in the manifest.
		return nil, fmt.Errorf("composer binary not found at %s", cachedComposerPath)
	}

	cachedPluginPath := getGoPluginPathFromManifest(manifest)
	if _, err := os.Stat(cachedPluginPath); os.IsNotExist(err) {
		// TODO(wbpcode): Download the plugin binary from the URL specified in the manifest.
		return nil, fmt.Errorf("go plugin binary not found at %s", cachedPluginPath)
	}

	// TODO(wbpcode): Support passing filter config to composer extensions.
	// Create New proto struct for Composer go plugin filter.
	configStruct, _ := structpb.NewStruct(map[string]any{
		"name": manifest.Name,
		"url":  cachedPluginPath,
	})

	// Covert to JSON string.
	configJSON, _ := protojson.Marshal(configStruct)

	// Convert JSON string to StringValue.
	configStringValue := wrapperspb.String(string(configJSON))

	// Covert the StringValue to Any.
	config, _ := anypb.New(configStringValue)

	protoConfig := &dymhttp.DynamicModuleFilter{
		DynamicModuleConfig: &dym.DynamicModuleConfig{
			Name:         "composer",
			LoadGlobally: true,
		},
		FilterName:   "goplugin",
		FilterConfig: config,
	}
	composerAny, err := anypb.New(protoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Composer filter to Any: %w", err)
	}

	return &hcmv3.HttpFilter{
		Name: manifest.Name,
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: composerAny,
		},
	}, nil
}
