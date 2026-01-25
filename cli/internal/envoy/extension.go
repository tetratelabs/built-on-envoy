// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"fmt"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

type (
	// ExtensionFilterGenerator defines an interface for generating filter configurations
	ExtensionFilterGenerator interface {
		// GenerateFilterConfig generates the filter configuration for the given extension manifest.
		GenerateFilterConfig(manifest *extensions.Manifest, config any) (*hcmv3.HttpFilter, error)
	}

	// LuaFilterGenerator generates filter configuration for Lua extensions.
	LuaFilterGenerator struct{}
	// WasmFilterGenerator generates filter configuration for Wasm extensions.
	WasmFilterGenerator struct{}
	// DynamicModuleFilterGenerator generates filter configuration for Dynamic Module extensions.
	DynamicModuleFilterGenerator struct{}
	// ComposerFilterGenerator generates filter configuration for Composer extensions.
	ComposerFilterGenerator struct{}
)

var (
	// ErrUnsupportedExtensionType is returned when an extension type is not supported.
	ErrUnsupportedExtensionType = fmt.Errorf("unsupported extension type")
	// ErrUnimplemented is returned when an extension filter generation is not yet implemented.
	ErrUnimplemented = fmt.Errorf("extension filter generation not yet implemented")
	// ErrMissingLuaCode is returned when a Lua extension manifest is missing inline code.
	ErrMissingLuaCode = fmt.Errorf("lua extension manifest must have inline Lua code")
)

// GenerateFilterConfig generates the filter configuration for the given extension manifest.
func GenerateFilterConfig(manifest *extensions.Manifest, config any) (*hcmv3.HttpFilter, error) {
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
func (l LuaFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, _ any) (*hcmv3.HttpFilter, error) {
	// TODO(nacx): Support loading from a file (no only inline). This cannot be done until
	// we implement the remote extension download to fetch extension contents, not only manifests.
	// Tracked in: https://github.com/tetratelabs/built-on-envoy/issues/16
	if manifest.Lua.Inline == "" {
		return nil, ErrMissingLuaCode
	}

	luaFilter := &luav3.Lua{
		DefaultSourceCode: &corev3.DataSource{
			Specifier: &corev3.DataSource_InlineString{
				InlineString: manifest.Lua.Inline,
			},
		},
	}
	luaAny, err := anypb.New(luaFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Lua filter to Any: %w", err)
	}

	return &hcmv3.HttpFilter{
		Name: manifest.Name,
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: luaAny,
		},
	}, nil
}

// GenerateFilterConfig generates the filter configuration for Wasm extensions.
func (w WasmFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("%w: wasm", ErrUnimplemented)
}

// GenerateFilterConfig generates the filter configuration for Dynamic Module extensions.
func (d DynamicModuleFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("%w: dynamic module", ErrUnimplemented)
}

// GenerateFilterConfig generates the filter configuration for Composer extensions.
func (c ComposerFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("%w: composer", ErrUnimplemented)
}
