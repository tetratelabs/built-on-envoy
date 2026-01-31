// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestParseCmdPullHelp(t *testing.T) {
	var cli struct {
		Pull Pull `cmd:"" help:"Pull an extension from an OCI registry"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"pull", "--help"})

	expected := `Usage: boe pull <extension> [flags]

Pull an extension from an OCI registry

Arguments:
  <extension>    Extension name or OCI repository URL (e.g., cors or
                 ghcr.io/tetratelabs/built-on-envoy/extension-cors:1.0.0)

Flags:
  -h, --help               Show context-sensitive help.

      --path=STRING        Destination path to extract the extension to.
      --insecure           Allow pulling from an insecure (HTTP) registry
                           (default: false)
      --username=STRING    Username for the OCI registry
                           ($BOE_REGISTRY_USERNAME).
      --password=STRING    Password for the OCI registry
                           ($BOE_REGISTRY_PASSWORD).
`
	require.Equal(t, expected, buf.String())
}

func TestParseExtensionReference(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantRepository string
		wantTag        string
		wantErr        error
	}{
		{
			name:           "simple extension name",
			input:          "cors",
			wantRepository: extensions.DefaultOCIRegistry + "/extension-cors",
			wantTag:        "latest",
		},
		{
			name:           "simple extension name with hyphen",
			input:          "request-logger",
			wantRepository: extensions.DefaultOCIRegistry + "/extension-request-logger",
			wantTag:        "latest",
		},
		{
			name:           "full OCI reference with tag",
			input:          "ghcr.io/tetratelabs/built-on-envoy/extension-cors:1.0.0",
			wantRepository: "ghcr.io/tetratelabs/built-on-envoy/extension-cors",
			wantTag:        "1.0.0",
		},
		{
			name:           "full OCI reference without tag",
			input:          "ghcr.io/tetratelabs/built-on-envoy/extension-cors",
			wantRepository: "ghcr.io/tetratelabs/built-on-envoy/extension-cors",
			wantTag:        "latest",
		},
		{
			name:           "localhost registry with port and tag",
			input:          "localhost:5000/my-extension:v1.2.3",
			wantRepository: "localhost:5000/my-extension",
			wantTag:        "v1.2.3",
		},
		{
			name:           "localhost registry with port without tag",
			input:          "localhost:5000/my-extension",
			wantRepository: "localhost:5000/my-extension",
			wantTag:        "latest",
		},
		{
			name:           "registry with port and nested path with tag",
			input:          "myregistry.io:8080/org/repo/extension:2.0.0",
			wantRepository: "myregistry.io:8080/org/repo/extension",
			wantTag:        "2.0.0",
		},
		{
			name:    "empty reference",
			input:   "",
			wantErr: errEmptyExtensionReference,
		},
		{
			name:    "reference with empty tag",
			input:   "ghcr.io/org/repo:",
			wantErr: errEmptyTag,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, tag, err := parseExtensionReference(tt.input)

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantRepository, repo)
			require.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestDownloadDirectory(t *testing.T) {
	dirs := &xdg.Directories{DataHome: "/home/user/.local/share"}
	p := &Pull{repository: "test", tag: "1.0.1"}

	require.Equal(t, "/home/user/.local/share/extensions/test/1.0.1", p.downloadDirectory(dirs))

	p.Path = "/custom/path"
	p.downloadDirectory(dirs)
	require.Equal(t, "/custom/path/extensions/test/1.0.1", p.downloadDirectory(dirs))
}
