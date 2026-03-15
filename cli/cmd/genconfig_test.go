// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
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
      --dev                        Whether to allow downloading dev versions of
                                   extensions (with -dev suffix). By default,
                                   only stable versions are allowed.
      --config=CONFIG              Optional JSON config string for extensions.
                                   Applied in order to combined --extension and
                                   --local flags.
      --cluster=CLUSTER,...        Optional additional Envoy cluster provided in
                                   the host:tlsPort pattern.
      --cluster-insecure=CLUSTER-INSECURE,...
                                   Optional additional Envoy cluster (with TLS
                                   transport disabled) provided in the host:port
                                   pattern.
      --cluster-json=CLUSTER-JSON
                                   Optional additional Envoy cluster providing
                                   the complete cluster config in JSON format.
      --registry="ghcr.io/tetratelabs/built-on-envoy"
                                   OCI registry URL for the extensions
                                   ($BOE_REGISTRY).
      --insecure                   Allow connecting to an insecure (HTTP)
                                   registry ($BOE_REGISTRY_INSECURE).
      --username=STRING            Username for the OCI registry
                                   ($BOE_REGISTRY_USERNAME).
      --password=STRING            Password for the OCI registry
                                   ($BOE_REGISTRY_PASSWORD).
      --test-upstream-host=STRING
                                   Hostname for the test upstream
                                   cluster. Mutually exclusive with
                                   --test-upstream-cluster. Defaults to
                                   "httpbin.org".
      --test-upstream-cluster=STRING
                                   Name of an existing configured cluster to
                                   use as the test upstream. The cluster must be
                                   configured via --cluster, --cluster-insecure,
                                   or --cluster-json. Mutually exclusive with
                                   --test-upstream-host.
      --output="-"                 Directory to put the generated config into.
                                   Use "-" to print it to the standard output.
`, internaltesting.WrapHelp(genConfigHelp))

	require.Equal(t, expected, buf.String())
}

func TestGenConfig(t *testing.T) {
	clusterJSON := `{"name":"my-cluster","type":"STRICT_DNS","load_assignment":{"cluster_name":"my-cluster","endpoints":[{"lb_endpoints":[{"endpoint":{"address":{"socket_address":{"address":"example.com","port_value":443}}}}]}]}}`
	clusterShort := `example.com:443`
	clusterInsecureShort := `example.com:80`

	tests := []struct {
		name                string
		minimal             bool
		local               []string
		clusters            []string
		clustersInsecure    []string
		clustersJSON        []string
		testUpstreamCluster string
		wantFile            string
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
			name:         "full config with JSON cluster",
			minimal:      false,
			local:        []string{"testdata/input_lua_inline"},
			clustersJSON: []string{clusterJSON},
			wantFile:     "testdata/output_full_config_with_cluster.yaml",
		},
		{
			name:         "only filters with JSON cluster",
			minimal:      true,
			local:        []string{"testdata/input_lua_inline"},
			clustersJSON: []string{clusterJSON},
			wantFile:     "testdata/output_only_filters_with_cluster.yaml",
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
		{
			name:             "full config with shorthand insecure cluster",
			minimal:          false,
			local:            []string{"testdata/input_lua_inline"},
			clustersInsecure: []string{clusterInsecureShort},
			wantFile:         "testdata/output_full_config_with_shorthand_insecure_cluster.yaml",
		},
		{
			name:             "only filters with shorthand insecure cluster",
			minimal:          true,
			local:            []string{"testdata/input_lua_inline"},
			clustersInsecure: []string{clusterInsecureShort},
			wantFile:         "testdata/output_only_filters_with_shorthand_insecure_cluster.yaml",
		},
		{
			name:             "full config with shorthand cluster and insecure cluster",
			minimal:          false,
			local:            []string{"testdata/input_lua_inline"},
			clusters:         []string{clusterShort},
			clustersInsecure: []string{clusterInsecureShort},
			wantFile:         "testdata/output_full_config_with_shorthand_cluster_and_insecure_cluster.yaml",
		},
		{
			name:             "only filters with shorthand cluster and insecure cluster",
			minimal:          true,
			local:            []string{"testdata/input_lua_inline"},
			clusters:         []string{clusterShort},
			clustersInsecure: []string{clusterInsecureShort},
			wantFile:         "testdata/output_only_filters_with_shorthand_cluster_and_insecure_cluster.yaml",
		},
		{
			name:                "full config with test upstream cluster",
			minimal:             false,
			local:               []string{"testdata/input_lua_inline"},
			clusters:            []string{clusterShort},
			testUpstreamCluster: clusterShort,
			wantFile:            "testdata/output_full_config_with_test_upstream_cluster.yaml",
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
				Clusters: ClusterFlags{
					Secure:   tt.clusters,
					Insecure: tt.clustersInsecure,
					JSONSpec: tt.clustersJSON,
				},
				TestUpstreamCluster: tt.testUpstreamCluster,
				Output:              "-",
				stdout:              &buf,
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

func TestGenConfigValidateMutualExclusion(t *testing.T) {
	cmd := &GenConfig{
		TestUpstreamHost:    "example.com",
		TestUpstreamCluster: "example.com:443",
	}
	require.ErrorContains(t, cmd.Validate(), "--test-upstream-host and --test-upstream-cluster are mutually exclusive")
}

func TestGenConfigMultipleArgsWithCommas(t *testing.T) {
	config1 := `{"header":"value1","header2":"value2"}`
	config2 := `{"another_config":"value3","yet_another_config":"value4"}`
	clusterJSON1 := `{"name":"cluster1","type":"STRICT_DNS","load_assignment":{"cluster_name":"cluster1"}}`
	clusterJSON2 := `{"name":"cluster2","type":"STRICT_DNS","load_assignment":{"cluster_name":"cluster2"}}`

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
		"--cluster-json", clusterJSON1, "--cluster-json", clusterJSON2,
	})
	require.NoError(t, err)
	require.Equal(t, []string{config1, config2}, cli.GenConfig.Configs)
	require.Equal(t, []string{clusterJSON1, clusterJSON2}, cli.GenConfig.Clusters.JSONSpec)
}

func TestGenConfigCreatesExportDir(t *testing.T) {
	var (
		buf    bytes.Buffer
		cmd    = &GenConfig{stdout: &buf, Output: "/dev/null"} // Force mkdir failure
		logger = internaltesting.NewTLogger(t)
		dirs   = &xdg.Directories{DataHome: t.TempDir()}
	)

	err := cmd.Run(t.Context(), dirs, logger)

	// We just wantto check that the command attempts to create the directory when the
	// output flag is provided. We just expect the operation to fail as we're using an
	// unwriteable path.
	var want *os.PathError
	require.ErrorAs(t, err, &want)
}

func TestGenConfigWriteConfig(t *testing.T) {
	var buf bytes.Buffer

	t.Run("write failure", func(t *testing.T) {
		var (
			cmd    = &GenConfig{stdout: &buf, Output: "/dev/null"} // Force write failure
			logger = internaltesting.NewTLogger(t)
			dirs   = &xdg.Directories{DataHome: t.TempDir()}
		)
		_, err := cmd.writeConfig("dummy", nil, dirs, logger)
		var want *os.PathError
		require.ErrorAs(t, err, &want)
	})

	t.Run("write success", func(t *testing.T) {
		var (
			cmd    = &GenConfig{stdout: &buf, Output: t.TempDir()}
			logger = internaltesting.NewTLogger(t)
			dirs   = &xdg.Directories{DataHome: t.TempDir()}

			mockRustExtension         = &extensions.Manifest{Name: "test-rust", Type: extensions.TypeRust}
			mockGoExtension           = &extensions.Manifest{Name: "test-go", Type: extensions.TypeGo, ComposerVersion: "1.0.0"}
			mockLuaExtension          = &extensions.Manifest{Name: "test-lua", Type: extensions.TypeLua}
			mockRustExtensionFile     = extensions.LocalCacheExtension(dirs, mockRustExtension)
			mockGoExtensionFile       = extensions.LocalCacheExtension(dirs, mockGoExtension)
			mockComposerExtensionFile = extensions.LocalCacheComposerLib(dirs, "1.0.0")
		)

		// Create the mock extensions at the source
		require.NoError(t, os.MkdirAll(filepath.Dir(mockRustExtensionFile), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Dir(mockGoExtensionFile), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Dir(mockComposerExtensionFile), 0o750))
		require.NoError(t, os.WriteFile(mockRustExtensionFile, []byte("mock rust"), 0o600))
		require.NoError(t, os.WriteFile(mockGoExtensionFile, []byte("mock go"), 0o600))
		require.NoError(t, os.WriteFile(mockComposerExtensionFile, []byte("mock go"), 0o600))

		_, err := cmd.writeConfig("dummy", []*extensions.Manifest{mockRustExtension, mockGoExtension, mockLuaExtension}, dirs, logger)
		require.NoError(t, err)
		require.FileExists(t, cmd.Output+"/envoy.yaml")
		require.FileExists(t, cmd.Output+"/libtest-rust.so")
		require.FileExists(t, cmd.Output+"/libcomposer.so")
		require.FileExists(t, cmd.Output+"/test-go.so")
	})
}

func TestPrintExportSummary(t *testing.T) {
	wantTemplate := `
%[1]v✓ Config exported to:%[2]v /tmp/boe-export
    - envoy.yaml
    - libcomposer.so

%[1]s→ Run locally with with func-e:%[2]s (https://func-e.io/)
    cd /tmp/boe-export
    export GODEBUG=cgocheck=0
    func-e run -c envoy.yaml --log-level info --component-log-level dynamic_modules:debug

%[1]s→ Run locally in Docker:%[2]s (not supported in Darwin hosts yet)
    docker run --rm \
        -p 10000:10000 \
        -p 9901:9901 \
        -e ENVOY_DYNAMIC_MODULES_SEARCH_PATH=/boe \
        -e GODEBUG=cgocheck=0 \
        -v /tmp/boe-export:/boe \
        -w /boe \
        envoyproxy/envoy:%[3]s -c /boe/envoy.yaml --log-level info --component-log-level dynamic_modules:debug
`

	t.Run("with Envoy version", func(t *testing.T) {
		var buf bytes.Buffer
		printExportSummary(&buf, "/tmp/boe-export", []string{"envoy.yaml", "libcomposer.so"}, 10000, 9901, "1.37.0")
		require.Equal(t, fmt.Sprintf(wantTemplate, internal.ANSIBold, internal.ANSIReset, "v1.37.0"), buf.String())
	})

	t.Run("without Envoy version defaults to 'dev' for Docker", func(t *testing.T) {
		var buf bytes.Buffer
		printExportSummary(&buf, "/tmp/boe-export", []string{"envoy.yaml", "libcomposer.so"}, 10000, 9901, "")
		require.Equal(t, fmt.Sprintf(wantTemplate, internal.ANSIBold, internal.ANSIReset, "dev"), buf.String())
	})
}
