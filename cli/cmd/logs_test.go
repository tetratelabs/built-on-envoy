// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestParseCmdLogsHelp(t *testing.T) {
	var cli struct {
		Logs Logs `cmd:"" help:"Print the CLI logs"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"logs", "--help"})

	expected := fmt.Sprintf(`Usage: boe logs [flags]

Print the CLI logs

%s
Flags:
  -h, --help        Show context-sensitive help.

  -f, --follow      Follow the log output (like tail -f).
  -t, --tail=INT    Number of recent log lines to show. Defaults to 20 when
                    --follow is set, 0 (all lines) otherwise.
`, internaltesting.WrapHelp(logsHelp))

	require.Equal(t, expected, buf.String())
}

func TestLogsNonExistentFile(t *testing.T) {
	dirs := &xdg.Directories{
		StateHome: filepath.Join(t.TempDir(), "nonexistent"),
	}
	var buf bytes.Buffer
	cmd := &Logs{output: &buf}

	require.NoError(t, cmd.Run(t.Context(), dirs, internaltesting.NewTLogger(t)))
	require.Empty(t, buf.String())
	require.FileExists(t, filepath.Join(dirs.StateHome, "boe.log"))
}

func TestLogsAllLines(t *testing.T) {
	dirs, logContent := newTestLogsState(t, 10)
	var buf bytes.Buffer
	cmd := &Logs{output: &buf} // Tail=0 means all lines

	require.NoError(t, cmd.Run(t.Context(), dirs, internaltesting.NewTLogger(t)))
	require.Equal(t, logContent, buf.String())
}

func TestLogsTail(t *testing.T) {
	dirs, _ := newTestLogsState(t, 10)
	var buf bytes.Buffer
	cmd := &Logs{Tail: 3, output: &buf}

	require.NoError(t, cmd.Run(t.Context(), dirs, internaltesting.NewTLogger(t)))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, "line 8", lines[0])
	require.Equal(t, "line 9", lines[1])
	require.Equal(t, "line 10", lines[2])
}

func TestLogsTailMoreThanAvailable(t *testing.T) {
	dirs, logContent := newTestLogsState(t, 5)
	var buf bytes.Buffer
	cmd := &Logs{Tail: 100, output: &buf}

	require.NoError(t, cmd.Run(t.Context(), dirs, internaltesting.NewTLogger(t)))
	require.Equal(t, logContent, buf.String())
}

func TestLogsAfterApply(t *testing.T) {
	t.Run("sets default tail when follow is set and tail is unset", func(t *testing.T) {
		cmd := &Logs{Follow: true}
		require.NoError(t, cmd.AfterApply())
		require.Equal(t, defaultFollowTail, cmd.Tail)
	})

	t.Run("does not change tail when follow is false", func(t *testing.T) {
		cmd := &Logs{Follow: false}
		require.NoError(t, cmd.AfterApply())
		require.Equal(t, 0, cmd.Tail)
	})

	t.Run("does not override an explicit tail", func(t *testing.T) {
		cmd := &Logs{Follow: true, Tail: 5}
		require.NoError(t, cmd.AfterApply())
		require.Equal(t, 5, cmd.Tail)
	})
}

func TestLogsFollow(t *testing.T) {
	dirs, _ := newTestLogsState(t, 30)
	logPath := filepath.Clean(filepath.Join(dirs.StateHome, "boe.log"))

	bufs := internaltesting.CaptureOutput("logs")
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	cmd := &Logs{Follow: true, output: bufs[0]} // no explicit Tail → AfterApply sets it to 20
	require.NoError(t, cmd.AfterApply())
	go func() {
		done <- cmd.Run(ctx, dirs, internaltesting.NewTLogger(t))
	}()

	// Wait until the goroutine has printed the default tail (last 20 lines).
	require.Eventually(t, func() bool {
		return strings.Contains(bufs[0].String(), "line 30")
	}, 2*time.Second, 10*time.Millisecond)

	// Write a new line while following.
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = fmt.Fprintln(f, "appended line")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.Eventually(t, func() bool {
		return strings.Contains(bufs[0].String(), "appended line")
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-done)

	out := bufs[0].String()
	// Should start at line 11 (last 20 of 30), not line 1.
	require.NotContains(t, out, "line 1\n")
	require.Contains(t, out, "line 11\n")
	require.Contains(t, out, "line 30\n")
	require.Contains(t, out, "appended line\n")
}

// newTestLogsState creates a temporary state directory with a boe.log file containing n lines.
// Returns the directories and the full file content (with trailing newline).
func newTestLogsState(t *testing.T, n int) (*xdg.Directories, string) {
	t.Helper()
	stateHome := t.TempDir()
	dirs := &xdg.Directories{StateHome: stateHome}

	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	content := sb.String()

	require.NoError(t, os.WriteFile(filepath.Join(stateHome, "boe.log"), []byte(content), 0o600))
	return dirs, content
}
