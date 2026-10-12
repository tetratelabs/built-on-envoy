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

// Debug is a command to run Envoy with local Go or Rust extensions built for debugging, with a debug
// server attached to the Envoy process.
type Debug struct {
	RunOpts         Run    `embed:""`
	DebugPort       uint32 `name:"debug-port" env:"BOE_DEBUG_PORT" help:"Port for the debug server attached to Envoy." default:"2345"`
	DebugServerPath string `name:"debug-server-path" env:"BOE_DEBUG_SERVER_PATH" help:"Path to the debug server binary: dlv for Go extensions (instead of the patched Delve that boe builds; unpatched versions may not be able to list goroutines), or gdbserver (Linux) or debugserver (macOS) for Rust extensions."`
	RebuildDelve    bool   `name:"rebuild-dlv" env:"BOE_REBUILD_DLV" help:"Rebuild the patched Delve used for Go extensions even if it is already in the cache."`

	// goos is the OS used to decide whether Envoy must run in a container. Overridable for testing.
	goos string `kong:"-"`
	// debugger is the debugger for the local extensions, set by Validate.
	debugger envoy.Debugger `kong:"-"`
}

//go:embed debug_help.md
var debugHelp string

// Help provides detailed help for the debug command.
func (d *Debug) Help() string { return debugHelp }

var (
	errDebugRemoteExtension = errors.New("boe debug only supports local extensions (--local)")
	errDebugNoLocal         = errors.New("boe debug requires at least one local extension (--local)")
	errDebugExtensionType   = errors.New("boe debug only supports Go, composer and Rust extensions")
	errDebugMixedTypes      = errors.New("boe debug cannot debug Go and Rust extensions at the same time")
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
	d.debugger = ""
	for _, local := range d.RunOpts.Local {
		debugger, err := debuggerFor(local)
		if err != nil {
			return err
		}
		if d.debugger != "" && d.debugger != debugger {
			return errDebugMixedTypes
		}
		d.debugger = debugger
	}
	if d.RebuildDelve && d.DebugServerPath != "" {
		return errors.New("--rebuild-dlv and --debug-server-path are mutually exclusive")
	}
	if d.debugger == envoy.DebuggerGDBRemote && d.RunOpts.Docker.Enabled {
		return errors.New("--docker is not supported for Rust extensions: they are debugged natively on all platforms")
	}
	if d.RebuildDelve && d.debugger != envoy.DebuggerDelve {
		return errors.New("--rebuild-dlv only applies to Go extensions")
	}
	if d.inContainer() && d.RunOpts.Envoy.Path != "" {
		return fmt.Errorf("--envoy-path is not supported when debugging Go extensions in a container (always the case on %s)", runtime.GOOS)
	}
	return nil
}

// Run executes the debug command.
func (d *Debug) Run(ctx context.Context, dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling debug command", "cmd", internal.RedactSensitive(d))
	d.RunOpts.Docker.Enabled = d.inContainer()
	return d.RunOpts.run(ctx, dirs, logger, &envoy.DebugOptions{
		Debugger:   d.debugger,
		Port:       d.DebugPort,
		ServerPath: d.DebugServerPath,
		Rebuild:    d.RebuildDelve,
		InstallDir: filepath.Join(dirs.DataHome, "tools"),
		// When running in a container, the notes computed in the host are passed in the environment.
		Notes: append(envoy.DebugNotesFromEnv(), symlinkHints(d.RunOpts.Local)...),
	})
}

// inContainer returns true if Envoy must run in a container to be debugged. Delve can only debug Go code
// loaded in a non-Go process on Linux, so on other platforms Go extensions are debugged in a Linux container.
// Native extensions (e.g. Rust) are always debugged natively, with a GDB remote protocol server.
func (d *Debug) inContainer() bool {
	goos := d.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	if d.debugger != envoy.DebuggerDelve {
		return false
	}
	return d.RunOpts.Docker.Enabled || goos != "linux"
}

// debuggerFor returns the debugger for the local extension at the given path: Delve for Go extensions and
// sub-extensions of a composer bundle, and the native debugger for Rust extensions.
func debuggerFor(path string) (envoy.Debugger, error) {
	manifest, err := extensions.LoadLocalManifest(filepath.Join(path, "manifest.yaml"))
	if err != nil {
		return "", fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
	}
	root := manifest
	if manifest.Parent != "" {
		if root, _, err = resolveParentNoFallback(manifest); err != nil {
			return "", fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
		}
	}
	switch root.Type {
	case extensions.TypeGo, extensions.TypeComposer:
		return envoy.DebuggerDelve, nil
	case extensions.TypeRust:
		return envoy.DebuggerGDBRemote, nil
	default:
		return "", fmt.Errorf("%w: %s is of type %q", errDebugExtensionType, manifest.Name, root.Type)
	}
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
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	resolvedAbs, err := filepath.EvalSymlinks(absolute)
	if err != nil || resolvedAbs == absolute {
		return "", "", false
	}
	// Strip the common trailing path elements to get the minimal mapping, but keep the symlink itself
	// (e.g. map /private/tmp to /tmp, not /private to /).
	for filepath.Base(absolute) == filepath.Base(resolvedAbs) {
		parentAbs, parentReal := filepath.Dir(absolute), filepath.Dir(resolvedAbs)
		if resolved, err := filepath.EvalSymlinks(parentAbs); err != nil || resolved != parentReal || parentAbs == parentReal {
			break
		}
		absolute, resolvedAbs = parentAbs, parentReal
	}
	return resolvedAbs, absolute, true
}

// mustEvalSymlinks returns the path with symlinks resolved, or the path itself if that fails.
func mustEvalSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
