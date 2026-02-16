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

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestParseCmdCleanHelp(t *testing.T) {
	var cli struct {
		Clean Clean `cmd:"" help:"Clean cache directories"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"clean", "--help"})

	expected := fmt.Sprintf(`Usage: boe clean [flags]

Clean cache directories

%s
Flags:
  -h, --help               Show context-sensitive help.

      --all                Clean all cache directories.
      --extension-cache    Clean the extension cache directory.
      --config-cache       Clean the config cache directory.
      --data-cache         Clean the data cache directory.
      --state-cache        Clean the state cache directory.
      --runtime-cache      Clean the runtime cache directory.
`, internaltesting.WrapHelp(cleanHelp))
	require.Equal(t, expected, buf.String())
}

func TestCleanAll(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{All: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, filepath.Join(dirs.DataHome, "extensions"))
	require.NoDirExists(t, dirs.ConfigHome)
	require.NoDirExists(t, dirs.DataHome)
	require.NoDirExists(t, dirs.StateHome)
	require.NoDirExists(t, dirs.RuntimeDir)
}

func TestCleanExtensionCache(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{ExtensionCache: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, filepath.Join(dirs.DataHome, "extensions"))
	// Other directories should still exist.
	require.DirExists(t, dirs.ConfigHome)
	require.DirExists(t, dirs.StateHome)
	require.DirExists(t, dirs.RuntimeDir)
}

func TestCleanConfigCache(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{ConfigCache: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, dirs.ConfigHome)
	// Other directories should still exist.
	require.DirExists(t, dirs.DataHome)
	require.DirExists(t, dirs.StateHome)
	require.DirExists(t, dirs.RuntimeDir)
}

func TestCleanDataCache(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{DataCache: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, dirs.DataHome)
	// Other directories should still exist.
	require.DirExists(t, dirs.ConfigHome)
	require.DirExists(t, dirs.StateHome)
	require.DirExists(t, dirs.RuntimeDir)
}

func TestCleanStateCache(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{StateCache: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, dirs.StateHome)
	// Other directories should still exist.
	require.DirExists(t, dirs.ConfigHome)
	require.DirExists(t, dirs.DataHome)
	require.DirExists(t, dirs.RuntimeDir)
}

func TestCleanRuntimeCache(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{RuntimeCache: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, dirs.RuntimeDir)
	// Other directories should still exist.
	require.DirExists(t, dirs.ConfigHome)
	require.DirExists(t, dirs.DataHome)
	require.DirExists(t, dirs.StateHome)
}

func TestCleanNoFlags(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{}

	require.NoError(t, cmd.Run(dirs))

	// All directories should still exist when no flags are set.
	require.DirExists(t, dirs.ConfigHome)
	require.DirExists(t, dirs.DataHome)
	require.DirExists(t, dirs.StateHome)
	require.DirExists(t, dirs.RuntimeDir)
}

func TestCleanMultipleFlags(t *testing.T) {
	dirs := newTestDirs(t)
	cmd := &Clean{ConfigCache: true, RuntimeCache: true}

	require.NoError(t, cmd.Run(dirs))

	require.NoDirExists(t, dirs.ConfigHome)
	require.NoDirExists(t, dirs.RuntimeDir)
	// Other directories should still exist.
	require.DirExists(t, dirs.DataHome)
	require.DirExists(t, dirs.StateHome)
}

func TestCleanNonExistentDirectories(t *testing.T) {
	// Use directories that don't exist on disk. os.RemoveAll on a
	// non-existent path returns nil, so this should succeed.
	dirs := &xdg.Directories{
		ConfigHome: filepath.Join(t.TempDir(), "nonexistent", "config"),
		DataHome:   filepath.Join(t.TempDir(), "nonexistent", "data"),
		StateHome:  filepath.Join(t.TempDir(), "nonexistent", "state"),
		RuntimeDir: filepath.Join(t.TempDir(), "nonexistent", "runtime"),
	}
	cmd := &Clean{All: true}

	require.NoError(t, cmd.Run(dirs))
}

// newTestDirs creates temporary XDG directories populated with a marker file
// so we can verify they exist before cleaning and are gone after.
func newTestDirs(t *testing.T) *xdg.Directories {
	t.Helper()

	base := t.TempDir()
	dirs := &xdg.Directories{
		ConfigHome: filepath.Join(base, "config"),
		DataHome:   filepath.Join(base, "data"),
		StateHome:  filepath.Join(base, "state"),
		RuntimeDir: filepath.Join(base, "runtime"),
	}

	// Create all directories and an extensions subdirectory under DataHome.
	for _, d := range []string{
		dirs.ConfigHome,
		dirs.DataHome,
		filepath.Join(dirs.DataHome, "extensions"),
		dirs.StateHome,
		dirs.RuntimeDir,
	} {
		require.NoError(t, os.MkdirAll(d, 0o750))
		// Write a marker file so the directory is non-empty.
		require.NoError(t, os.WriteFile(filepath.Join(d, "marker"), []byte("test"), 0o600))
	}

	return dirs
}
