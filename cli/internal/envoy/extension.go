// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"fmt"

	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

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

// generateFilterConfig generates the filter configuration for the given extension manifest.
func generateFilterConfig(manifest *extensions.Manifest, config any) (*hcmv3.HttpFilter, error) {
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
		return nil, fmt.Errorf("unsupported extension type: %s", manifest.Type)
	}

	return generator.GenerateFilterConfig(manifest, config)
}

// GenerateFilterConfig generates the filter configuration for Lua extensions.
func (l LuaFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("lua extension filter generation not implemented yet")
}

// GenerateFilterConfig generates the filter configuration for Wasm extensions.
func (w WasmFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("wasm extension filter generation not implemented yet")
}

// GenerateFilterConfig generates the filter configuration for Dynamic Module extensions.
func (d DynamicModuleFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("dynamic module extension filter generation not implemented yet")
}

// GenerateFilterConfig generates the filter configuration for Composer extensions.
func (c ComposerFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (*hcmv3.HttpFilter, error) {
	return nil, fmt.Errorf("composer extension filter generation not implemented yet")
}
