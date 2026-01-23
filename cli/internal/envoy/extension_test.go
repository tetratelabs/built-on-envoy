// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestGenerateFilterConfig(t *testing.T) {
	tests := []struct {
		name     string
		manifest extensions.Manifest
		want     any
	}{
		{
			name:     "Lua filter",
			manifest: extensions.Manifest{Type: extensions.TypeLua},
			want:     "lua",
		},
		{
			name:     "Wasm filter",
			manifest: extensions.Manifest{Type: extensions.TypeWasm},
			want:     "wasm",
		},
		{
			name:     "Dynamic Module filter",
			manifest: extensions.Manifest{Type: extensions.TypeDynamicModule},
			want:     "dynamic_module",
		},
		{
			name:     "Dynamic Module filter",
			manifest: extensions.Manifest{Type: extensions.TypeDynamicModule},
			want:     "dynamic_module",
		},
		{
			name:     "Composer filter",
			manifest: extensions.Manifest{Type: extensions.TypeComposer},
			want:     "composer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateFilterConfig(&tt.manifest, nil)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
