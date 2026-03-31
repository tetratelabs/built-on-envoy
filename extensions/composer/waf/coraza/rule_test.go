// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapPath(t *testing.T) {
	for _, tc := range []struct {
		input, expected string
		expectedOK      bool
	}{
		{
			input:      "@recommended.conf",
			expected:   "rules/recommended.conf",
			expectedOK: true,
		},
		{
			input:      "@recommended-conf",
			expected:   "rules/recommended.conf",
			expectedOK: true,
		},
		{
			input:      "@ftw.conf",
			expected:   "rules/ftw.conf",
			expectedOK: true,
		},
		{
			input:      "@ftw-conf",
			expected:   "rules/ftw.conf",
			expectedOK: true,
		},
		{
			input:      "@crs-setup.conf",
			expected:   "rules/crs-setup.conf",
			expectedOK: true,
		},
		{
			input:      "@crs-setup-conf",
			expected:   "rules/crs-setup.conf",
			expectedOK: true,
		},
		{
			input:      "/tmp/unknown",
			expected:   "",
			expectedOK: false,
		},
	} {
		result, ok := mapPath(tc.input)
		require.Equal(t, tc.expected, result)
		require.Equal(t, tc.expectedOK, ok)
	}
}

func TestRulesFSOpen(t *testing.T) {
	r := rulesFS{}

	file, err := r.Open("@recommended.conf")
	require.NoError(t, err)
	require.NotNil(t, file)

	file, err = r.Open("unknown")
	require.Error(t, err)
	require.Nil(t, file)
}

func TestRulesFSReadFile(t *testing.T) {
	r := rulesFS{}

	content, err := r.ReadFile("@ftw.conf")
	require.NoError(t, err)
	require.NotNil(t, content)

	content, err = r.ReadFile("unknown")
	require.Error(t, err)
	require.Nil(t, content)
}
