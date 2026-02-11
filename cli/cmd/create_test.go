// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestParseCmdCreateHelp(t *testing.T) {
	var cli struct {
		Create Create `cmd:"" help:"Create a new extension template."`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"create", "--help"})

	expected := fmt.Sprintf(`Usage: boe create <name> [flags]

Create a new extension template.

%s
Arguments:
  <name>    Name of the extension.

Flags:
  -h, --help               Show context-sensitive help.

      --type="composer"    Type of the extension. Currently only 'composer' is
                           supported.
      --path=STRING        Output directory for the extension. Defaults to the
                           extension name.
`, internaltesting.WrapHelp(createHelp))
	require.Equal(t, expected, buf.String())
}

func TestCreate_Run(t *testing.T) {
	// Ensure go is available as the command runs `go mod tidy`
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not found in PATH")
	}

	tmpDir := t.TempDir()
	name := "my-extension"

	c := &Create{
		Type: "composer",
		Name: name,
		Path: tmpDir,
	}

	// This might fail if network is not available due to `go mod tidy`
	// failing to fetch dependencies.
	err := c.Run(&xdg.Directories{})
	if err != nil {
		// Attempt to differentiate network error from logic error if possible,
		// but for now we'll just fail the test if the command fails.
		// Use t.Log to provide context on failure.
		require.NoErrorf(t, err, " Create.Run failed: %v", err)

		repoPath := filepath.Join(tmpDir, name)
		require.DirExists(t, repoPath)

		files := []string{
			"plugin.go",
			"manifest.yaml",
			"Makefile",
			"go.mod",
			"Dockerfile",
			"Dockerfile.code",
			".dockerignore",
			"embedded/host.go",
			"standalone/main.go",
		}
		for _, f := range files {
			require.FileExists(t, filepath.Join(repoPath, f))
		}

		// verify manifest.yaml content
		// #nosec G304
		manifest, err := os.ReadFile(filepath.Join(repoPath, "manifest.yaml"))
		require.NoError(t, err)
		assert.Contains(t, string(manifest), "name: "+name)

		// verify plugin.go content
		// #nosec G304
		plugin, err := os.ReadFile(filepath.Join(repoPath, "plugin.go"))
		require.NoError(t, err)
		assert.Contains(t, string(plugin), "x-"+name)
		assert.Contains(t, string(plugin), "WellKnownHttpFilterConfigFactories")
	}
}

func TestUnsupportedType(t *testing.T) {
	c := &Create{
		Type: "unsupported-type",
		Name: "test-extension",
	}

	err := c.Run(&xdg.Directories{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported extension type")
}
