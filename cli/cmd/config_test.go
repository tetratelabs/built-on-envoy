// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestParseCmdGenConfigHelp(t *testing.T) {
	var cli struct {
		GenConfig GenConfig `cmd:"" help:"Generate Envoy configuration with specified extensions"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"gen-config", "--help"})

	expected := `Usage: boe gen-config [flags]

Generate Envoy configuration with specified extensions

The gen-config command generates Envoy configuration YAML for the specified
extensions. This is useful for inspecting the generated configuration,
integrating with existing Envoy deployments, or using with external Envoy
management tools.

By default, it outputs a complete Envoy bootstrap configuration. Use the
` + "`--only-filters`" + ` flag to generate just the HTTP filter chain configuration,
which can be embedded into an existing ` + "`HttpConnectionManager`" + ` configuration.

Flags:
  -h, --help                       Show context-sensitive help.

      --only-filters               Generate configuration with only extension
                                   filters.
      --listen-port=10000          Port for Envoy listener to accept incoming
                                   traffic.
      --admin-port=9901            Port for Envoy admin interface.
      --extension=EXTENSION,...    Extensions to enable (by name).
      --local=LOCAL                Path to a directory containing a local
                                   Extension to enable.
`
	require.Equal(t, expected, buf.String())
}

func TestGenConfig(t *testing.T) {
	tests := []struct {
		name       string
		onlyFilter bool
		manifests  []string
		wantFile   string
	}{
		{
			name:       "only filters",
			onlyFilter: true,
			manifests:  []string{"testdata/input_lua_inline.yaml"},
			wantFile:   "testdata/output_only_filters.yaml",
		},
		{
			name:       "full config",
			onlyFilter: false,
			manifests:  []string{"testdata/input_lua_inline.yaml"},
			wantFile:   "testdata/output_full_config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cmd := &GenConfig{
				OnlyFilters: tt.onlyFilter,
				AdminPort:   9901,
				ListenPort:  10000,
				extensions:  []*extensions.Manifest{},
				output:      &buf,
			}
			for _, manifestPath := range tt.manifests {
				cmd.extensions = append(cmd.extensions, mustReadManifest(t, manifestPath))
			}

			require.NoError(t, cmd.Run())

			want, err := os.ReadFile(tt.wantFile)
			require.NoError(t, err)

			require.YAMLEq(t, string(want), buf.String())
		})
	}
}

func TestGenConfigError(t *testing.T) {
	cmd := &GenConfig{
		extensions: []*extensions.Manifest{
			{Type: "unsupported_type"},
		},
	}
	require.ErrorIs(t, cmd.Run(), envoy.ErrUnsupportedExtensionType)
}

func mustReadManifest(t *testing.T, path string) *extensions.Manifest {
	manifest, err := extensions.LoadLocalManifest(path)
	require.NoError(t, err)
	return manifest
}
