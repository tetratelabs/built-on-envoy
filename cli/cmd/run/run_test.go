// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package run

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestRunCommandHelp(t *testing.T) {
	var cli struct {
		Run Cmd `cmd:"" help:"Run Envoy with extensions"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("ee"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"run", "--help"})

	expected := `Usage: ee run [flags]

Run Envoy with extensions

Flags:
  -h, --help                     Show context-sensitive help.

      --envoy-version=STRING     Envoy version to use (e.g., 1.31.0)
                                 ($ENVOY_VERSION)
  -l, --log-level="all:error"    Envoy component log level (default: all:error)
      --run-id=STRING            Run identifier for this invocation. Defaults to
                                 timestamp-based ID or $EE_RUN_ID. Use '0' for
                                 Docker/Kubernetes ($EE_RUN_ID).
      --listen-port=10000        Port for Envoy listener to accept incoming
                                 traffic (default: 10000)
      --admin-port=9901          Port for Envoy admin interface (default: 9901)
`
	require.Equal(t, expected, buf.String())
}

func TestRunCommandDefaults(t *testing.T) {
	var cli struct {
		Run Cmd `cmd:"" help:"Run Envoy with extensions"`
	}

	parser, err := kong.New(&cli, kong.Name("ee"), kong.Exit(func(int) {}))
	require.NoError(t, err)

	_, err = parser.Parse([]string{"run"})
	require.NoError(t, err)

	// Verify default values are set
	require.Equal(t, "all:error", cli.Run.LogLevel)
	require.Equal(t, 10000, cli.Run.ListenPort)
	require.Equal(t, 9901, cli.Run.AdminPort)
	require.Empty(t, cli.Run.EnvoyVersion)

	// Verify RunID is generated with expected format: YYYYMMDD_HHMMSS_UUU
	require.NotEmpty(t, cli.Run.RunID)
	require.Regexp(t, `^\d{8}_\d{6}_\d{3}$`, cli.Run.RunID)
}

func TestRunCommandCustomValues(t *testing.T) {
	var cli struct {
		Run Cmd `cmd:"" help:"Run Envoy with extensions"`
	}

	parser, err := kong.New(&cli, kong.Name("ee"), kong.Exit(func(int) {}))
	require.NoError(t, err)

	_, err = parser.Parse([]string{
		"run",
		"--log-level=all:debug,upstream:trace",
		"--listen-port=8080",
		"--admin-port=9000",
		"--envoy-version=1.31.0",
		"--run-id=custom-run-id",
	})
	require.NoError(t, err)

	require.Equal(t, "all:debug,upstream:trace", cli.Run.LogLevel)
	require.Equal(t, 8080, cli.Run.ListenPort)
	require.Equal(t, 9000, cli.Run.AdminPort)
	require.Equal(t, "1.31.0", cli.Run.EnvoyVersion)
	require.Equal(t, "custom-run-id", cli.Run.RunID)
}
