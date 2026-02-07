// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/docker"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
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
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"push", "--help"})

	expected := fmt.Sprintf(`Usage: boe push <local extension> [flags]

Push an extension to an OCI registry

%s
Arguments:
  <local extension>    Path to a directory containing the extension to push.

Flags:
  -h, --help               Show context-sensitive help.

      --registry="ghcr.io/tetratelabs/built-on-envoy"
                           OCI registry URL for the extensions ($BOE_REGISTRY).
      --insecure           Allow connecting to an insecure (HTTP) registry
                           ($BOE_REGISTRY_INSECURE).
      --username=STRING    Username for the OCI registry
                           ($BOE_REGISTRY_USERNAME).
      --password=STRING    Password for the OCI registry
                           ($BOE_REGISTRY_PASSWORD).
      --build              Build and push Docker image with pre-compiled
                           plugin.so using Docker buildx.
      --platforms="linux/amd64,linux/arm64"
                           Target platforms (comma-separated). Supported:
                           linux/amd64, linux/arm64
`, internaltesting.WrapHelp(pushHelp))

	require.Equal(t, expected, buf.String())
}

func TestPushValidate(t *testing.T) {
	tests := []struct {
		name    string
		local   string
		build   bool
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
		{
			name:  "lua extension without build flag",
			local: "testdata",
			build: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Push{Local: tt.local, Build: tt.build}
			err := p.Validate()

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, p.manifest)
				require.NotEmpty(t, p.manifest.Name)
				require.NotEmpty(t, p.manifest.Version)
			}
		})
	}
}

func TestPushValidate_BuildFlagOnlyForComposer(t *testing.T) {
	// Test that build flag returns error for non-composer types
	p := &Push{Local: "testdata", Build: true}
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--build flag only supported for composer type")
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
			client, err := newOCIRepositoryClient(tt.repository, tt.username, tt.password, tt.insecure)
			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.wantErr, client == nil)
		})
	}
}

func TestPushAfterApply(t *testing.T) {
	// Test AfterApply method
	p := &Push{
		Local: "testdata",
		OCI: OCIFlags{
			Registry: "ghcr.io/test",
			Username: "user",
			Password: "pass",
		},
	}

	// Need to validate first to load manifest
	err := p.Validate()
	require.NoError(t, err)

	// Test AfterApply
	err = p.AfterApply(nil)
	require.NoError(t, err)

	// Verify srcReference is set
	require.Contains(t, p.srcReference, "src-")
	require.NotNil(t, p.client)
}

func TestPushAfterApply_WithBuild(t *testing.T) {
	// Test AfterApply with build flag (should fail validation for non-composer)
	p := &Push{
		Local: "testdata",
		Build: true,
		OCI: OCIFlags{
			Registry: "ghcr.io/test",
		},
	}

	// Should fail validation because testdata is not composer type
	err := p.Validate()
	require.Error(t, err)
}

func TestPushLocalGoExtension(t *testing.T) {
	dataDir := t.TempDir()

	ctx := t.Context()

	checkDockerBuildxErr := docker.CheckDockerBuildx(ctx)
	if checkDockerBuildxErr != nil {
		t.Skipf("Skipping test because Docker Buildx is not available: %v", checkDockerBuildxErr)
	}

	// Create a brand new extension
	c := &Create{
		Name: "go-e2e",
		Path: dataDir,
		Type: string(extensions.TypeComposer),
	}
	require.NoError(t, c.Run(&xdg.Directories{DataHome: dataDir}), "failed to create extension")

	// Start a local OCI registry
	container, registry, err := internaltesting.StartOCIRegistry(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	// Check host architecture to speed up the test to avoid qemu overhead
	// for unsupported architectures
	var platforms string
	switch runtime.GOARCH {
	case "amd64":
		platforms = "linux/amd64"
	default:
		platforms = "linux/arm64"
	}

	p := &Push{
		Local: dataDir + "/go-e2e",
		Build: true,
		OCI: OCIFlags{
			Registry: registry + "/test",
			Insecure: true,
		},
		Platforms: platforms,
	}
	_ = p.Validate()
	_ = p.AfterApply(nil)

	ctxWithDryRun := context.WithValue(ctx, docker.ExtensionBuildxDryRun{}, true)
	_ = p.Run(ctxWithDryRun)
}
