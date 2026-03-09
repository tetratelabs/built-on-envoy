// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestExpandPath(t *testing.T) {
	// Get current working directory for absolute path tests
	cwd, err := os.Getwd()
	require.NoError(t, err)

	// Get HOME for tilde expansion tests
	home := os.Getenv("HOME")
	require.NotEmpty(t, home, "HOME environment variable must be set for tests")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "tilde expansion",
			input:    "~/.config/boe",
			expected: filepath.Join(home, ".config", "boe"),
		},
		{
			name:     "absolute path unchanged",
			input:    "/usr/local/bin",
			expected: "/usr/local/bin",
		},
		{
			name:     "relative path becomes absolute",
			input:    "relative/path",
			expected: filepath.Join(cwd, "relative", "path"),
		},
		{
			name:     "environment variable expansion",
			input:    "${HOME}/test",
			expected: filepath.Join(home, "test"),
		},
		{
			name:     "multiple environment variables",
			input:    "${HOME}/${USER}",
			expected: filepath.Join(home, os.Getenv("USER")),
		},
		{
			name:     "tilde only without slash is not expanded",
			input:    "~notauser",
			expected: filepath.Join(cwd, "~notauser"),
		},
		{
			name:     "dot path becomes absolute",
			input:    ".",
			expected: cwd,
		},
		{
			name:     "double dot path becomes absolute",
			input:    "..",
			expected: filepath.Dir(cwd),
		},
		{
			name:     "undefined env var expands to empty",
			input:    "${UNDEFINED_VAR_12345}/path",
			expected: "/path", // undefined var becomes empty, resulting in absolute path /path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, expandPath(tt.input))
		})
	}
}

func TestExpandPathWithDifferentHOME(t *testing.T) {
	// Set a custom HOME
	customHome := "/custom/home"
	t.Setenv("HOME", customHome)

	result := expandPath("~/.config")
	require.Equal(t, filepath.Join(customHome, ".config"), result)
}

func TestParseCmdRootHelp(t *testing.T) {
	var cli CLI

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Description("Built On Envoy CLI - Discover, run, and build custom filters with zero friction"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"--help"})

	expected := `Usage: boe <command> [flags]

Built On Envoy CLI - Discover, run, and build custom filters with zero friction

Flags:
  -h, --help                     Show context-sensitive help.
      --config-home="~/.config/boe"
                                 Configuration files directory. Defaults to
                                 ~/.config/boe ($BOE_CONFIG_HOME)
      --data-home="~/.local/share/boe"
                                 Downloaded Envoy binaries directory. Defaults
                                 to ~/.local/share/boe ($BOE_DATA_HOME)
      --state-home="~/.local/state/boe"
                                 Persistent state and logs directory. Defaults
                                 to ~/.local/state/boe ($BOE_STATE_HOME)
      --runtime-dir=STRING       Ephemeral runtime files directory. Defaults to
                                 /tmp/boe-$UID ($BOE_RUNTIME_DIR)
      --boe-log-level="debug"    Log level for the CLI. Defaults to debug
                                 ($BOE_LOG_LEVEL)

Commands:
  list [flags]
    List available extensions

  run [flags]
    Run Envoy with extensions

  gen-config [flags]
    Generate Envoy configuration with extensions

  create <name> [flags]
    Create a new extension template

  clean [flags]
    Clean cache directories

  version [flags]
    Print version information

Run "boe <command> --help" for more information on a command.
`
	require.Equal(t, expected, buf.String())
}

func TestParseCmdVersionHelp(t *testing.T) {
	var cli CLI

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Description("Built On Envoy CLI - Discover, run, and build custom filters with zero friction"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"version", "--help"})

	expected := `Usage: boe version [flags]

Print version information

Print the version information for the Built On Envoy CLI.

Flags:
  -h, --help                     Show context-sensitive help.
      --config-home="~/.config/boe"
                                 Configuration files directory. Defaults to
                                 ~/.config/boe ($BOE_CONFIG_HOME)
      --data-home="~/.local/share/boe"
                                 Downloaded Envoy binaries directory. Defaults
                                 to ~/.local/share/boe ($BOE_DATA_HOME)
      --state-home="~/.local/state/boe"
                                 Persistent state and logs directory. Defaults
                                 to ~/.local/state/boe ($BOE_STATE_HOME)
      --runtime-dir=STRING       Ephemeral runtime files directory. Defaults to
                                 /tmp/boe-$UID ($BOE_RUNTIME_DIR)
      --boe-log-level="debug"    Log level for the CLI. Defaults to debug
                                 ($BOE_LOG_LEVEL)
`
	require.Equal(t, expected, buf.String())
}

func TestVersionRun(t *testing.T) {
	var buf bytes.Buffer
	v := &Version{output: &buf}
	require.NoError(t, v.Run())
	require.Equal(t, "Built On Envoy CLI: dev\n", buf.String())
}
