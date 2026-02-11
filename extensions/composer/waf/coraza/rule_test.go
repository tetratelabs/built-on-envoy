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
	}{
		{
			input:    "@recommended-conf",
			expected: "rules/recommended.conf",
		},
		{
			input:    "@ftw-conf",
			expected: "rules/ftw.conf",
		},
		{
			input:    "@crs-setup-conf",
			expected: "rules/crs-setup.conf",
		},
		{
			input:    "@owasp_crs/test.conf",
			expected: "rules/owasp_crs/test.conf",
		},
		{
			input:    "@owasp_crs/*.conf",
			expected: "rules/owasp_crs/*.conf",
		},
		{
			input: "unknown",
		},
	} {
		result, err := mapPath(tc.input)
		if tc.expected == "" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		}
	}
}

func TestRulesFSOpen(t *testing.T) {
	r := rulesFS{}

	file, err := r.Open("@recommended-conf")
	require.NoError(t, err)
	require.NotNil(t, file)

	file, err = r.Open("unknown")
	require.Error(t, err)
	require.Nil(t, file)
}

func TestRulesFSReadDir(t *testing.T) {
	r := rulesFS{}

	entries, err := r.ReadDir("@owasp_crs")
	require.NoError(t, err)
	require.NotNil(t, entries)

	entries, err = r.ReadDir("unknown")
	require.Error(t, err)
	require.Nil(t, entries)
}

func TestRulesFSReadFile(t *testing.T) {
	r := rulesFS{}

	content, err := r.ReadFile("@ftw-conf")
	require.NoError(t, err)
	require.NotNil(t, content)

	content, err = r.ReadFile("unknown")
	require.Error(t, err)
	require.Nil(t, content)
}
