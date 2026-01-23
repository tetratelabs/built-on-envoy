// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestParseCmdListHelp(t *testing.T) {
	var cli struct {
		List List `cmd:"" help:"List available extensions"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"list", "--help"})

	expected := `Usage: boe list

List available extensions

Flags:
  -h, --help    Show context-sensitive help.
`
	require.Equal(t, expected, buf.String())
}

func TestListCommand(t *testing.T) {
	var buf bytes.Buffer
	cmd := &List{output: &buf}

	err := cmd.Run()
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(output, "\n")

	headers := strings.Fields(lines[0])
	require.Len(t, headers, 4)
	require.Equal(t, []string{"NAME", "VERSION", "TYPE", "DESCRIPTION"}, headers)

	// Verify all extensions are listed
	names := make(map[string]struct{})
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		fields := fieldsN(line, 4)
		m, ok := extensions.Manifests[fields[0]]

		require.Truef(t, ok, "extension %s not found in manifests", fields[0])
		require.Equal(t,
			[]string{m.Name, m.Version, string(m.Type), truncateDescription(m.Description, 60)},
			fields,
		)

		names[m.Name] = struct{}{}
	}
	require.Len(t, names, len(extensions.Manifests))
}

func fieldsN(s string, n int) []string {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []string{strings.TrimSpace(s)}
	}

	fields := strings.Fields(s)
	if len(fields) <= n {
		return fields
	}
	return append(fields[:n-1], strings.Join(fields[n-1:], " "))
}

func TestListCommandAlphabeticalOrder(t *testing.T) {
	var buf bytes.Buffer
	cmd := &List{output: &buf}

	err := cmd.Run()
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(output, "\n")

	// Skip header line and empty lines
	var names []string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		// First column is the name (before first whitespace sequence)
		fields := strings.Fields(line)
		if len(fields) > 0 {
			names = append(names, fields[0])
		}
	}

	// Verify names are in alphabetical order
	for i := 1; i < len(names); i++ {
		require.LessOrEqual(t, names[i-1], names[i],
			"extensions should be in alphabetical order: %s should come before %s", names[i-1], names[i])
	}
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		name     string
		desc     string
		maxLen   int
		expected string
	}{
		{
			name:     "short description unchanged",
			desc:     "A short desc",
			maxLen:   20,
			expected: "A short desc",
		},
		{
			name:     "exact length unchanged",
			desc:     "Exactly twenty chars",
			maxLen:   20,
			expected: "Exactly twenty chars",
		},
		{
			name:     "long description truncated",
			desc:     "This is a very long description that should be truncated",
			maxLen:   20,
			expected: "This is a very lo...",
		},
		{
			name:     "newlines replaced with spaces",
			desc:     "Line one\nLine two",
			maxLen:   50,
			expected: "Line one Line two",
		},
		{
			name:     "whitespace trimmed",
			desc:     "  trimmed  ",
			maxLen:   50,
			expected: "trimmed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateDescription(tt.desc, tt.maxLen)
			require.Equal(t, tt.expected, result)
		})
	}
}
