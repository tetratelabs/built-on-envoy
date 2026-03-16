// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

var errInvalidPlatform = fmt.Errorf("invalid platform format, expected os/arch")

// Download is a command to download extensions.
type Download struct {
	Extension string   `arg:"" help:"The name of the extension to download. For example, 'example-go'."`
	Platform  string   `name:"platform" help:"The target platform for the extension in the format os/arch. For example, 'linux/amd64'. If not specified, it defaults to the current platform."`
	Dev       bool     `help:"Whether to allow downloading dev versions of extensions (with -dev suffix). By default, only stable versions are allowed." default:"false"`
	Path      string   `name:"path" help:"Directory to put the downloaded extension artifact into. Defaults to the current directory." default:"." type:"path"`
	OCI       OCIFlags `embed:""`

	downloader *extensions.Downloader `kong:"-"` // Internal field for the downloader
}

//go:embed download_help.md
var downloadHelp string

// Help provides detailed help for the download command.
func (d *Download) Help() string { return downloadHelp }

// AfterApply validates the command flags and initializes the downloader.
func (d *Download) AfterApply(dirs *xdg.Directories, logger *slog.Logger) error {
	platformOS, platformArch, err := getPlatform(d.Platform)
	if err != nil {
		return err
	}
	// Override the download path to the specified one.
	dirs.DataHome = d.Path
	d.downloader = &extensions.Downloader{
		Logger:                logger,
		Registry:              d.OCI.Registry,
		Username:              d.OCI.Username,
		Password:              d.OCI.Password,
		Insecure:              d.OCI.Insecure,
		Dirs:                  dirs,
		OS:                    platformOS,
		Arch:                  platformArch,
		DevVersions:           d.Dev,
		DisableSourceFallback: true,
	}
	return nil
}

// Run executes the Download command.
func (d *Download) Run(ctx context.Context, logger *slog.Logger) error {
	logger.Debug("handling download command", "cmd", internal.RedactSensitive(d))

	name, tag := splitRef(d.Extension)
	_, _ = fmt.Fprintf(os.Stderr, "→ %sDownloading %s for %s...%s\n",
		internal.ANSIBold, d.Extension, d.Platform, internal.ANSIReset)

	var (
		downloaded extensions.DownloadedExtension
		err        error
	)
	switch name {
	case extensions.ComposerArtifact, extensions.ComposerArtifactLite, extensions.ComposerArtifactSource:
		downloaded, err = d.downloader.DownloadComposer(ctx, tag, name)
	default:
		downloaded, err = d.downloader.DownloadExtension(ctx, name, tag)
	}
	if err != nil {
		return fmt.Errorf("failed to download extension: %w", err)
	}

	fmt.Printf("Extension downloaded to: %s\n", downloaded.Path)

	return nil
}

// getPlatform parses the platform string in the format os/arch and returns the os and arch separately.
// If the platform string is empty, it returns the current platform.
func getPlatform(platform string) (string, string, error) {
	if platform == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	platformOS, platformArch, ok := strings.Cut(platform, "/")
	if !ok {
		return "", "", fmt.Errorf("%w: %s", errInvalidPlatform, platform)
	}
	return platformOS, platformArch, nil
}
