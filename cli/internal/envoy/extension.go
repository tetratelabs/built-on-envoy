// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"fmt"

	"github.com/tetratelabs/envoy-ecosystem/cli/internal/extensions"
)

type (
	// ExtensionFilterGenerator defines an interface for generating filter configurations
	ExtensionFilterGenerator interface {
		// GenerateFilterConfig generates the filter configuration for the given extension manifest.
		// TODO(nacx): come up with return type: do we want to return a string? do we
		// want to return go-control-plane types?
		GenerateFilterConfig(manifest *extensions.Manifest, config any) (any, error)
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
func generateFilterConfig(manifest *extensions.Manifest, config any) (any, error) {
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
func (l LuaFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (any, error) {
	// TODO(nacx): implement Lua filter config generation
	return "lua", nil
}

// GenerateFilterConfig generates the filter configuration for Wasm extensions.
func (w WasmFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (any, error) {
	// TODO(nacx): implement Wasm filter config generation
	return "wasm", nil
}

// GenerateFilterConfig generates the filter configuration for Dynamic Module extensions.
func (d DynamicModuleFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (any, error) {
	// TODO(nacx): implement Dynamic Module filter config generation
	return "dynamic_module", nil
}

// GenerateFilterConfig generates the filter configuration for Composer extensions.
func (c ComposerFilterGenerator) GenerateFilterConfig(*extensions.Manifest, any) (any, error) {
	// TODO(nacx): implement Composer filter config generation
	return "composer", nil
}
