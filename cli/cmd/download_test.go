// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"

	"github.com/alecthomas/kong"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestParseCmdDownloadHelp(t *testing.T) {
	var cli struct {
		Download Download `cmd:"" help:"Download an extension."`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"download", "--help"})

	expected := fmt.Sprintf(`Usage: boe download <extension> [flags]

Download an extension.

%s
Arguments:
  <extension>    The name of the extension to download. For example,
                 'example-go'.

Flags:
  -h, --help               Show context-sensitive help.

      --platform=STRING    The target platform for the extension in the
                           format os/arch. For example, 'linux/amd64'. If not
                           specified, it defaults to the current platform.
      --dev                Whether to allow downloading dev versions of
                           extensions (with -dev suffix). By default, only
                           stable versions are allowed.
      --path="."           Directory to put the downloaded extension artifact
                           into. Defaults to the current directory.
      --registry="%s"
                           OCI registry URL for the extensions ($BOE_REGISTRY).
      --insecure           Allow connecting to an insecure (HTTP) registry
                           ($BOE_REGISTRY_INSECURE).
      --username=STRING    Username for the OCI registry
                           ($BOE_REGISTRY_USERNAME).
      --password=STRING    Password for the OCI registry
                           ($BOE_REGISTRY_PASSWORD).
`, internaltesting.WrapHelp(downloadHelp), extensions.DefaultOCIRegistry)
	require.Equal(t, expected, buf.String())
}

func TestAfterApply(t *testing.T) {
	tests := []struct {
		platform    string
		path        string
		wantOS      string
		wantArch    string
		wantDataDir string
		wantErr     error
	}{
		{"", "", runtime.GOOS, runtime.GOARCH, "", nil},
		{"linux/arm64", "/tmp/download", "linux", "arm64", "/tmp/download", nil},
		{"invalid", "/tmp", "", "", "", errInvalidPlatform},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			cmd := &Download{Platform: tt.platform, Path: tt.path}
			err := cmd.AfterApply(&xdg.Directories{}, internaltesting.NewTLogger(t))

			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				require.Equal(t, tt.wantOS, cmd.downloader.OS)
				require.Equal(t, tt.wantArch, cmd.downloader.Arch)
				require.Equal(t, tt.wantDataDir, cmd.downloader.Dirs.DataHome)
			}
		})
	}
}

func TestRunDownload(t *testing.T) {
	t.Run("unexisting extension", func(t *testing.T) {
		cmd := &Download{
			Extension:  "non-existing-extension",
			Platform:   "linux/amd64",
			downloader: newTestDownloader(t, t.TempDir(), &mockOCIClient{}),
		}

		err := cmd.Run(t.Context(), cmd.downloader.Logger)
		require.ErrorContains(t, err, "failed to resolve latest tag")
	})

	t.Run("download composer", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:                 "composer",
				extensions.OCIAnnotationExtensionType:   string(extensions.TypeGo),
				extensions.OCIAnnotationArtifact:        extensions.ArtifactBinary,
				extensions.OCIAnnotationComposerVersion: "1.0.0",
				ocispec.AnnotationVersion:               "1.0.0",
			},
		}

		cmd := &Download{
			Extension:  "composer:1.0.0",
			Platform:   "linux/amd64",
			downloader: newTestDownloader(t, t.TempDir(), mock),
		}

		err := cmd.Run(t.Context(), cmd.downloader.Logger)
		require.NoError(t, err)
	})

	t.Run("download extension", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-ext",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeRust),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
				ocispec.AnnotationVersion:             "1.0.0",
			},
		}

		cmd := &Download{
			Extension:  "my-ext:1.0.0",
			Platform:   "linux/amd64",
			downloader: newTestDownloader(t, t.TempDir(), mock),
		}

		err := cmd.Run(t.Context(), cmd.downloader.Logger)
		require.NoError(t, err)
	})
}
