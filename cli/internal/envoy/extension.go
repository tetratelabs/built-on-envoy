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
	dymv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/dynamic_modules/v3"
	dymhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_modules/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

type (
	// ExtensionFilterGenerator defines an interface for generating filter configurations
	ExtensionFilterGenerator interface {
		// GenerateFilterConfig generates the filter configuration for the given extension manifest.
		GenerateFilterConfig(manifest *extensions.Manifest, dirs *xdg.Directories, config string) (*ExtensionResources, error)
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
func GenerateFilterConfig(manifest *extensions.Manifest, dirs *xdg.Directories, config string) (*ExtensionResources, error) {
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

	return generator.GenerateFilterConfig(manifest, dirs, config)
}

// GenerateFilterConfig generates the filter configuration for Lua extensions.
func (l LuaFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, _ *xdg.Directories, _ string) (*ExtensionResources, error) {
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
func (w WasmFilterGenerator) GenerateFilterConfig(*extensions.Manifest, *xdg.Directories, string) (*ExtensionResources, error) {
	return nil, fmt.Errorf("%w: wasm", ErrUnimplemented)
}

// GenerateFilterConfig generates the filter configuration for Dynamic Module extensions.
func (d DynamicModuleFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest,
	_ *xdg.Directories, config string,
) (*ExtensionResources, error) {
	var anyConfig *anypb.Any

	if config != "" {
		// Convert JSON string to StringValue.
		// Ideally we suggest that `config` should be JSON string. But Envoy's DynamicModuleFilter
		// take a string value anyway. And it's possible that a user wants to pass a non-JSON string.
		// We pass the string as-is to Envoy anyway and let the dynamic module handle the content.
		configStringValue := wrapperspb.String(config)
		// Covert the StringValue to Any.
		anyConfig, _ = anypb.New(configStringValue)
	}

	// Use the library name (with underscores) as the dynamic module config name.
	// This is the identifier Envoy uses to reference the loaded module.
	protoConfig := &dymhttpv3.DynamicModuleFilter{
		DynamicModuleConfig: &dymv3.DynamicModuleConfig{
			Name:         manifest.Name,
			LoadGlobally: false,
		},
		FilterName:   manifest.Name,
		FilterConfig: anyConfig,
	}
	dynamicModuleAny, err := anypb.New(protoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dynamic module filter to Any: %w", err)
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: dynamicModuleAny,
				},
			},
		},
	}, nil
}

// GenerateFilterConfig generates the filter configuration for Composer extensions.
func (c ComposerFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, dirs *xdg.Directories, config string) (*ExtensionResources, error) {
	cachedComposerPath := extensions.LocalCacheComposerLib(dirs, manifest.ComposerVersion, !manifest.Remote)
	if _, err := os.Stat(cachedComposerPath); os.IsNotExist(err) {
		// TODO(wbpcode): Download the composer binary from the URL specified in the manifest.
		return nil, fmt.Errorf("composer binary not found at %s", cachedComposerPath)
	}

	cachedPluginPath := extensions.LocalCacheExtension(dirs, manifest)
	if _, err := os.Stat(cachedPluginPath); os.IsNotExist(err) {
		// TODO(wbpcode): Download the plugin binary from the URL specified in the manifest.
		return nil, fmt.Errorf("go plugin binary not found at %s", cachedPluginPath)
	}

	// Covert the config to struct first. For go plugin/composer extensions, we ensure the
	// config is always a valid JSON string (could be converted to google.protobuf.Struct).
	var configValue *structpb.Value
	if config != "" {
		innerStruct := &structpb.Struct{}
		err := protojson.Unmarshal([]byte(config), innerStruct)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal config JSON string to Struct: %w", err)
		}
		configValue = structpb.NewStructValue(innerStruct)
	} else {
		configValue = structpb.NewNullValue()
	}

	// Create New proto struct for Composer go plugin filter.
	configStruct := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"name":   structpb.NewStringValue(manifest.Name),
			"url":    structpb.NewStringValue("file://" + cachedPluginPath),
			"config": configValue,
			// TODO(wbpcode): this could be false always in local testing/development.
			// Should we support to configure this or give different default value for
			// `run` and `genconfig` command?
			"strict_check": structpb.NewBoolValue(false),
		},
	}

	// Covert to JSON string.
	configJSON, _ := protojson.Marshal(configStruct)

	// Convert JSON string to StringValue.
	configStringValue := wrapperspb.String(string(configJSON))

	// Covert the StringValue to Any.
	anyConfig, _ := anypb.New(configStringValue)

	protoConfig := &dymhttpv3.DynamicModuleFilter{
		DynamicModuleConfig: &dymv3.DynamicModuleConfig{
			Name:         "composer",
			LoadGlobally: true,
		},
		FilterName:   "goplugin",
		FilterConfig: anyConfig,
	}
	composerAny, err := anypb.New(protoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Composer filter to Any: %w", err)
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: composerAny,
				},
			},
		},
	}, nil
}
