// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package run

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/envoy-ecosystem/cli/internal/extensions"
)

func TestParseCommandHelp(t *testing.T) {
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
  -h, --help                       Show context-sensitive help.

      --envoy-version=STRING       Envoy version to use (e.g., 1.31.0)
                                   ($ENVOY_VERSION)
  -l, --log-level="all:error"      Envoy component log level (default:
                                   all:error)
      --run-id=STRING              Run identifier for this invocation.
                                   Defaults to timestamp-based ID or $EE_RUN_ID.
                                   Use '0' for Docker/Kubernetes ($EE_RUN_ID).
      --listen-port=10000          Port for Envoy listener to accept incoming
                                   traffic (default: 10000)
      --admin-port=9901            Port for Envoy admin interface (default:
                                   9901)
      --extension=EXTENSION,...    Extensions to enable (by name).
`
	require.Equal(t, expected, buf.String())
}

func TestParseCommandDefaults(t *testing.T) {
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
	require.Empty(t, cli.Run.Extensions)

	// Verify RunID is generated with expected format: YYYYMMDD_HHMMSS_UUU
	require.NotEmpty(t, cli.Run.RunID)
	require.Regexp(t, `^\d{8}_\d{6}_\d{3}$`, cli.Run.RunID)
}

func TestParseCommandCustomValues(t *testing.T) {
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
		"--extension=cors,rate-limiter",
		"--extension=auth-jwt",
	})
	require.NoError(t, err)

	require.Equal(t, "all:debug,upstream:trace", cli.Run.LogLevel)
	require.Equal(t, 8080, cli.Run.ListenPort)
	require.Equal(t, 9000, cli.Run.AdminPort)
	require.Equal(t, "1.31.0", cli.Run.EnvoyVersion)
	require.Equal(t, "custom-run-id", cli.Run.RunID)
	require.Equal(t, []string{"cors", "rate-limiter", "auth-jwt"}, cli.Run.Extensions)
}

func TestParseInvalidExtension(t *testing.T) {
	available := slices.Collect(maps.Keys(extensions.Manifests))
	sort.Strings(available)

	var cli struct {
		Run Cmd `cmd:"" help:"Run Envoy with extensions"`
	}

	parser, err := kong.New(&cli, kong.Name("ee"), kong.Exit(func(int) {}))
	require.NoError(t, err)

	_, err = parser.Parse([]string{"run", "--extension=unknown-extension"})

	require.EqualError(t, err,
		fmt.Sprintf(`run: unknown extension "unknown-extension"; available extensions: %s`,
			strings.Join(available, ",")))
}

func TestValidateLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		logLevel  string
		wantError string
	}{
		{
			name:     "empty log level is valid",
			logLevel: "",
		},
		{
			name:     "single component and level",
			logLevel: "all:error",
		},
		{
			name:     "multiple components",
			logLevel: "all:error,upstream:trace,http:debug",
		},
		{
			name:     "whitespace is trimmed",
			logLevel: " all:error , upstream:trace ",
		},
		{
			name:      "empty entry",
			logLevel:  " all:error,,upstream:trace ",
			wantError: `invalid log level format "": expected component:level`,
		},
		{
			name:      "missing colon separator",
			logLevel:  "allerror",
			wantError: `invalid log level format "allerror": expected component:level`,
		},
		{
			name:      "empty component",
			logLevel:  ":error",
			wantError: `invalid log level format ":error": component cannot be empty`,
		},
		{
			name:      "empty level",
			logLevel:  "all:",
			wantError: `invalid log level format "all:": level cannot be empty`,
		},
		{
			name:      "whitespace-only component",
			logLevel:  " :error",
			wantError: `invalid log level format " :error": component cannot be empty`,
		},
		{
			name:      "whitespace-only level",
			logLevel:  "all: ",
			wantError: `invalid log level format "all: ": level cannot be empty`,
		},
		{
			name:      "missing colon in multi-component string",
			logLevel:  "all:error,badformat",
			wantError: `invalid log level format "badformat": expected component:level`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := Cmd{LogLevel: tt.logLevel}
			err := cmd.Validate()
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantError)
			}
		})
	}
}

func TestParseLogLevels(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		wantBaseLevel      string
		wantComponentLevel string
	}{
		{
			name:               "empty string defaults to warning",
			input:              "",
			wantBaseLevel:      "error",
			wantComponentLevel: "",
		},
		{
			name:               "all component only",
			input:              "all:debug",
			wantBaseLevel:      "debug",
			wantComponentLevel: "",
		},
		{
			name:               "single component without all",
			input:              "upstream:debug",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug",
		},
		{
			name:               "multiple components without all",
			input:              "upstream:debug,connection:trace",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "all with other components",
			input:              "all:info,upstream:debug,connection:trace",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "all at the end",
			input:              "upstream:debug,all:error",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug",
		},
		{
			name:               "all in the middle",
			input:              "upstream:debug,all:info,connection:trace",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "handles whitespace",
			input:              " upstream:debug , all:info , connection:trace ",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// errors are tested in TestValidateLogLevel
			baseLevel, componentLevels, _ := parseLogLevels(tt.input)
			require.Equal(t, tt.wantBaseLevel, baseLevel)
			require.Equal(t, tt.wantComponentLevel, componentLevels)
		})
	}
}
