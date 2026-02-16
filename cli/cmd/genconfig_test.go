// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
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
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"gen-config", "--help"})

	expected := fmt.Sprintf(`Usage: boe gen-config [flags]

Generate Envoy configuration with specified extensions

%s
Flags:
  -h, --help                       Show context-sensitive help.

      --minimal                    Generate configuration with only
                                   extension-generated resources (HTTP filters
                                   and clusters).
      --listen-port=10000          Port for Envoy listener to accept incoming
                                   traffic.
      --admin-port=9901            Port for Envoy admin interface.
      --extension=EXTENSION,...    Extensions to enable (in the format: "name"
                                   or "name:version").
      --local=LOCAL                Path to a directory containing a local
                                   Extension to enable.
      --config=CONFIG              Optional JSON config string for extensions.
                                   Applied in order to combined --extension and
                                   --local flags.
      --cluster=CLUSTER            Optional additional Envoy cluster. Supports
                                   JSON or short format (host:tlsPort).
      --registry="ghcr.io/tetratelabs/built-on-envoy"
                                   OCI registry URL for the extensions
                                   ($BOE_REGISTRY).
      --insecure                   Allow connecting to an insecure (HTTP)
                                   registry ($BOE_REGISTRY_INSECURE).
      --username=STRING            Username for the OCI registry
                                   ($BOE_REGISTRY_USERNAME).
      --password=STRING            Password for the OCI registry
                                   ($BOE_REGISTRY_PASSWORD).
`, internaltesting.WrapHelp(genConfigHelp))

	require.Equal(t, expected, buf.String())
}

func TestGenConfig(t *testing.T) {
	clusterJSON := `{"name":"my-cluster","type":"STRICT_DNS","load_assignment":{"cluster_name":"my-cluster","endpoints":[{"lb_endpoints":[{"endpoint":{"address":{"socket_address":{"address":"example.com","port_value":443}}}}]}]}}`
	clusterShort := `example.com:443`

	tests := []struct {
		name     string
		minimal  bool
		local    []string
		clusters []string
		wantFile string
	}{
		{
			name:     "only filters",
			minimal:  true,
			local:    []string{"testdata/input_lua_inline"},
			wantFile: "testdata/output_only_filters.yaml",
		},
		{
			name:     "full config",
			minimal:  false,
			local:    []string{"testdata/input_lua_inline"},
			wantFile: "testdata/output_full_config.yaml",
		},
		{
			name:     "full config with JSON cluster",
			minimal:  false,
			local:    []string{"testdata/input_lua_inline"},
			clusters: []string{clusterJSON},
			wantFile: "testdata/output_full_config_with_cluster.yaml",
		},
		{
			name:     "only filters with JSON cluster",
			minimal:  true,
			local:    []string{"testdata/input_lua_inline"},
			clusters: []string{clusterJSON},
			wantFile: "testdata/output_only_filters_with_cluster.yaml",
		},
		{
			name:     "full config with shorthand cluster",
			minimal:  false,
			local:    []string{"testdata/input_lua_inline"},
			clusters: []string{clusterShort},
			wantFile: "testdata/output_full_config_with_shorthand_cluster.yaml",
		},
		{
			name:     "only filters with shorthand cluster",
			minimal:  true,
			local:    []string{"testdata/input_lua_inline"},
			clusters: []string{clusterShort},
			wantFile: "testdata/output_only_filters_with_shorthand_cluster.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cmd := &GenConfig{
				Minimal:    tt.minimal,
				AdminPort:  9901,
				ListenPort: 10000,
				Local:      tt.local,
				Clusters:   tt.clusters,
				output:     &buf,
			}

			var args []string
			var err error
			for _, local := range tt.local {
				args = append(args, "--local", local)
			}
			cmd.extensionPositions, err = saveExtensionPositions(args)
			require.NoError(t, err)

			logger := internaltesting.NewTLogger(t)
			dirs := &xdg.Directories{DataHome: t.TempDir()}
			require.NoError(t, cmd.Run(t.Context(), dirs, logger))

			want, err := os.ReadFile(tt.wantFile)
			require.NoError(t, err)

			require.YAMLEq(t, string(want), buf.String())
		})
	}
}

func TestGenConfigMultipleArgsWithCommas(t *testing.T) {
	config1 := `{"header":"value1","header2":"value2"}`
	config2 := `{"another_config":"value3","yet_another_config":"value4"}`
	cluster1 := `{"name":"cluster1","type":"STRICT_DNS","load_assignment":{"cluster_name":"cluster1"}}`
	cluster2 := `{"name":"cluster2","type":"STRICT_DNS","load_assignment":{"cluster_name":"cluster2"}}`

	var cli struct {
		GenConfig GenConfig `cmd:"" help:"Generate Envoy configuration with specified extensions"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, err = parser.Parse([]string{
		"gen-config",
		"--config", config1, "--config", config2,
		"--cluster", cluster1, "--cluster", cluster2,
	})
	require.NoError(t, err)
	require.Equal(t, []string{config1, config2}, cli.GenConfig.Configs)
	require.Equal(t, []string{cluster1, cluster2}, cli.GenConfig.Clusters)
}
