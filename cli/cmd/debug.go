// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Debug is a command to run Envoy with local Go extensions built for debugging, with a
// headless Delve server attached to the Envoy process.
type Debug struct {
	RunOpts   Run    `embed:""`
	DelvePort uint32 `name:"dlv-port" env:"BOE_DLV_PORT" help:"Port for the headless Delve server attached to Envoy." default:"2345"`
	DelvePath string `name:"dlv-path" env:"BOE_DLV_PATH" help:"Path to a dlv binary to use instead of the patched Delve that boe builds. Unpatched Delve versions may not be able to list goroutines."`

	// goos is the OS used to decide whether Envoy must run in a container. Overridable for testing.
	goos string `kong:"-"`
}

//go:embed debug_help.md
var debugHelp string

// Help provides detailed help for the debug command.
func (d *Debug) Help() string { return debugHelp }

var (
	errDebugRemoteExtension = errors.New("boe debug only supports local extensions (--local)")
	errDebugNoLocal         = errors.New("boe debug requires at least one local extension (--local)")
	errDebugExtensionType   = errors.New("boe debug only supports Go and composer extensions")
)

// BeforeApply is called by Kong before applying the flag values. Envoy may run in a container
// (see inContainer), so the Docker-specific flags (e.g. --docker-image-version) are accepted by the
// embedded run options even if --docker is not set.
func (d *Debug) BeforeApply() error {
	d.RunOpts.dockerImplicit = true
	return nil
}

// Validate is called by Kong after parsing to validate the command arguments.
// The embedded run options are validated by Kong as well.
func (d *Debug) Validate() error {
	if len(d.RunOpts.Extensions) > 0 {
		return errDebugRemoteExtension
	}
	if len(d.RunOpts.Local) == 0 {
		return errDebugNoLocal
	}
	if d.inContainer() && d.RunOpts.Envoy.Path != "" {
		return fmt.Errorf("--envoy-path is not supported when debugging in a container (always the case on %s)", runtime.GOOS)
	}
	for _, local := range d.RunOpts.Local {
		if err := validateDebuggableExtension(local); err != nil {
			return err
		}
	}
	return nil
}

// Run executes the debug command.
func (d *Debug) Run(ctx context.Context, dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling debug command", "cmd", internal.RedactSensitive(d))
	d.RunOpts.Docker.Enabled = d.inContainer()
	return d.RunOpts.run(ctx, dirs, logger, &envoy.DebugOptions{
		DelvePort:  d.DelvePort,
		DelvePath:  d.DelvePath,
		InstallDir: filepath.Join(dirs.DataHome, "tools"),
	})
}

// inContainer returns true if Envoy must run in a container to be debugged. Delve can only debug Go code
// loaded in a non-Go process on Linux, so on other platforms Envoy and Delve run in a Linux container.
func (d *Debug) inContainer() bool {
	goos := d.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	return d.RunOpts.Docker.Enabled || goos != "linux"
}

// validateDebuggableExtension checks that the local extension at the given path is a Go extension, or a
// sub-extension of a composer bundle.
func validateDebuggableExtension(path string) error {
	manifest, err := extensions.LoadLocalManifest(filepath.Join(path, "manifest.yaml"))
	if err != nil {
		return fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
	}
	root := manifest
	if manifest.Parent != "" {
		if root, _, err = resolveParentNoFallback(manifest); err != nil {
			return fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
		}
	}
	if root.Type != extensions.TypeGo && root.Type != extensions.TypeComposer {
		return fmt.Errorf("%w: %s is of type %q", errDebugExtensionType, manifest.Name, root.Type)
	}
	return nil
}
