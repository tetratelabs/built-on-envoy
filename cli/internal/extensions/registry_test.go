// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryName(t *testing.T) {
	require.Equal(t,
		"ghcr.io/tetratelabs/built-on-envoy/extension-sample",
		RepositoryName(DefaultOCIRegistry, "sample"))
}

func TestNameFromRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		want       string
	}{
		{
			name:       "full repository URL with extension prefix",
			repository: "ghcr.io/tetratelabs/built-on-envoy/extension-cors",
			want:       "cors",
		},
		{
			name:       "repository without extension prefix",
			repository: "ghcr.io/tetratelabs/built-on-envoy/cors",
			want:       "cors",
		},
		{
			name:       "simple name with extension prefix",
			repository: "extension-sample",
			want:       "sample",
		},
		{
			name:       "simple name without extension prefix",
			repository: "sample",
			want:       "sample",
		},
		{
			name:       "empty string",
			repository: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NameFromRepository(tt.repository))
		})
	}
}
