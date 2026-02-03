// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	expected := `Usage: boe create <name> [flags]

Create a new extension template.

The create command generates a new extension template with the specified name
and type. This is useful for getting started with developing a new extension for
Built On Envoy.

By default, it creates a 'composer' type extension, which is an HTTP filter
extension. The generated template includes boilerplate code, a manifest file,
and a Makefile to help you build and install the extension.

You can specify the output directory using the --path flag. If not specified,
it defaults to a directory named after the extension.

Arguments:
  <name>    Name of the extension.

Flags:
  -h, --help               Show context-sensitive help.

      --type="composer"    Type of the extension. Currently only 'composer' is
                           supported.
      --path=STRING        Output directory for the extension. Defaults to the
                           extension name.
`
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
	err := c.Run()
	if err != nil {
		// Attempt to differentiate network error from logic error if possible,
		// but for now we'll just fail the test if the command fails.
		// Use t.Log to provide context on failure.
	require.NoErrorf(t, err," Create.Run failed: %v", err)

	repoPath := filepath.Join(tmpDir, name)
	require.DirExists(t, repoPath)

	files := []string{"plugin.go", "manifest.yaml", "Makefile", "go.mod"}
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
