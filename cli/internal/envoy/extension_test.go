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
		wantErr  string
	}{
		{
			name:     "Lua filter",
			manifest: extensions.Manifest{Type: extensions.TypeLua},
			wantErr:  "lua extension filter generation not implemented yet",
		},
		{
			name:     "Wasm filter",
			manifest: extensions.Manifest{Type: extensions.TypeWasm},
			wantErr:  "wasm extension filter generation not implemented yet",
		},
		{
			name:     "Dynamic Module filter",
			manifest: extensions.Manifest{Type: extensions.TypeDynamicModule},
			wantErr:  "dynamic module extension filter generation not implemented yet",
		},
		{
			name:     "Composer filter",
			manifest: extensions.Manifest{Type: extensions.TypeComposer},
			wantErr:  "composer extension filter generation not implemented yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateFilterConfig(&tt.manifest, nil)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}
