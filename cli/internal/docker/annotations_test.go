// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnnotationPrefix(t *testing.T) {
	tests := []struct {
		name          string
		platformCount int
		expected      string
	}{
		{
			name:          "single platform",
			platformCount: 1,
			expected:      "",
		},
		{
			name:          "two platforms",
			platformCount: 2,
			expected:      "index,manifest:",
		},
		{
			name:          "three platforms",
			platformCount: 3,
			expected:      "index,manifest:",
		},
		{
			name:          "zero platforms",
			platformCount: 0,
			expected:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnnotationPrefix(tt.platformCount)
			require.Equal(t, tt.expected, result)
		})
	}
}
