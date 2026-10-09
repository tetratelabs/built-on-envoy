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
		// When running in a container, the notes computed in the host are passed in the environment.
		Notes: append(envoy.DebugNotesFromEnv(), symlinkHints(d.RunOpts.Local)...),
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

// symlinkHints returns warnings about local extension paths that go through a symlink (e.g. /tmp on macOS is a
// symlink to /private/tmp). The debug information records the path the extension was built from, and
// Delve only resolves breakpoints set with that exact path, but IDEs may set breakpoints using the
// resolved path. In that case, the IDE needs a path mapping.
func symlinkHints(paths []string) []string {
	var (
		hints []string
		seen  = make(map[string]bool)
	)
	for _, path := range paths {
		from, to, ok := symlinkSubstitutePath(path)
		if !ok || seen[from] {
			continue
		}
		seen[from] = true
		hints = append(hints, fmt.Sprintf(`%[1]s⚠ %[3]s resolves to %[4]s through a symlink.%[2]s
  Breakpoints only resolve with paths under %[5]s. If your IDE uses %[6]s, add a path mapping.
  For VS Code, add this to the attach launch configuration:
    "substitutePath": [{ "from": %[6]q, "to": %[5]q }]
`, internal.ANSIBold, internal.ANSIReset, path, mustEvalSymlinks(path), to, from))
	}
	return hints
}

// symlinkSubstitutePath returns the path prefixes that differ between the given path and the path with
// its symlinks resolved: from is the resolved prefix and to the prefix recorded in the debug information.
// For example, for /tmp/ext on macOS, it returns ("/private/tmp", "/tmp").
func symlinkSubstitutePath(path string) (from, to string, ok bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || real == abs {
		return "", "", false
	}
	// Strip the common trailing path elements to get the minimal mapping, but keep the symlink itself
	// (e.g. map /private/tmp to /tmp, not /private to /).
	for filepath.Base(abs) == filepath.Base(real) {
		parentAbs, parentReal := filepath.Dir(abs), filepath.Dir(real)
		if resolved, err := filepath.EvalSymlinks(parentAbs); err != nil || resolved != parentReal || parentAbs == parentReal {
			break
		}
		abs, real = parentAbs, parentReal
	}
	return real, abs, true
}

// mustEvalSymlinks returns the path with symlinks resolved, or the path itself if that fails.
func mustEvalSymlinks(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
