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
)

func TestParseCmdPushHelp(t *testing.T) {
	var cli struct {
		Push Push `cmd:"" help:"Push an extension to an OCI registry"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"push", "--help"})

	expected := `Usage: boe push <local extension> [flags]

Push an extension to an OCI registry

Arguments:
  <local extension>    Path to a directory containing the extension to push.

Flags:
  -h, --help               Show context-sensitive help.

      --registry="ghcr.io/tetratelabs/built-on-envoy"
                           OCI registry URL to push the extension to. (default:
                           ghcr.io/tetratelabs/built-on-envoy) ($BOE_REGISTRY)
      --insecure           Allow pushing to an insecure (HTTP) registry
                           (default: false)
      --username=STRING    Username for the OCI registry
                           ($BOE_REGISTRY_USERNAME).
      --password=STRING    Password for the OCI registry
                           ($BOE_REGISTRY_PASSWORD).
`
	require.Equal(t, expected, buf.String())
}

func TestPushValidate(t *testing.T) {
	tests := []struct {
		name    string
		local   string
		wantErr error
	}{
		{
			name:  "valid extension directory",
			local: "testdata",
		},
		{
			name:    "directory without manifest",
			local:   ".",
			wantErr: extensions.ErrOpenManifestFile,
		},
		{
			name:    "non-existent directory",
			local:   "testdata/non-existent",
			wantErr: extensions.ErrOpenManifestFile,
		},
		{
			name:    "invalid manifest schema",
			local:   "testdata/invalid_manifest",
			wantErr: errInvalidManifest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Push{Local: tt.local}
			err := p.Validate()

			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantErr == nil {
				require.NotNil(t, p.manifest)
				require.NotEmpty(t, p.manifest.Name)
				require.NotEmpty(t, p.manifest.Version)
			}
		})
	}
}

func TestNewOCIClient(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		username   string
		password   string
		insecure   bool
		wantErr    bool
	}{
		{
			name:       "valid repository",
			repository: "ghcr.io/tetratelabs/built-on-envoy/extension-test",
		},
		{
			name:       "valid repository with credentials",
			repository: "ghcr.io/tetratelabs/built-on-envoy/extension-test",
			username:   "user",
			password:   "pass",
		},
		{
			name:       "valid repository with insecure",
			repository: "localhost:5000/extension-test",
			insecure:   true,
		},
		{
			name:       "valid repository with credentials and insecure",
			repository: "localhost:5000/extension-test",
			username:   "user",
			password:   "pass",
			insecure:   true,
		},
		{
			name:       "username only sets credentials",
			repository: "ghcr.io/org/repo",
			username:   "user",
		},
		{
			name:       "password only sets credentials",
			repository: "ghcr.io/org/repo",
			password:   "pass",
		},
		{
			name:       "invalid repository reference",
			repository: "://invalid",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := newOCIClient(tt.repository, tt.username, tt.password, tt.insecure)
			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.wantErr, client == nil)
		})
	}
}
