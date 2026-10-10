// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
)

const (
	// DefaultDebugPort is the default port for the debug server.
	DefaultDebugPort = 2345
	// DebugListenHostEnv overrides the address the debug server listens on. Set by RunnerDocker so the
	// debug server listens on all interfaces inside the container.
	DebugListenHostEnv = "BOE_DEBUG_LISTEN_HOST"
	// debugNotesEnv passes the debug notes computed in the host to the boe process running in the container.
	debugNotesEnv = "BOE_DEBUG_NOTES"
	// debugNotesSeparator separates the debug notes in debugNotesEnv.
	debugNotesSeparator = "\x1e"

	// envoyProcessTimeout is how long to wait for the Envoy process to be found.
	envoyProcessTimeout = 30 * time.Second
)

// debugServerReadyTimeout is how long to wait for the debug server to accept connections. Overridable for testing.
var debugServerReadyTimeout = 60 * time.Second

// Debugger is the kind of debugger attached to Envoy.
type Debugger string

const (
	// DebuggerDelve debugs Go extensions with a headless Delve server.
	DebuggerDelve Debugger = "delve"
	// DebuggerGDBRemote debugs native extensions (e.g. Rust) with a server speaking the GDB remote serial
	// protocol: gdbserver on Linux, and LLDB's debugserver on macOS.
	DebuggerGDBRemote Debugger = "gdbremote"
)

// DebugOptions configures attaching a debugger to the Envoy process.
type DebugOptions struct {
	// Debugger is the kind of debugger to attach.
	Debugger Debugger
	// Port is the port the debug server listens on.
	Port uint32
	// ServerPath is the path to the debug server binary: dlv for Delve, gdbserver or debugserver for the GDB
	// remote debugger. If empty, boe builds a patched Delve in InstallDir, or looks for the GDB remote debug server.
	ServerPath string
	// InstallDir is the directory where boe builds Delve.
	InstallDir string
	// Rebuild forces rebuilding Delve even if it is already in InstallDir.
	Rebuild bool
	// Notes are additional messages printed once Envoy is ready.
	Notes []string

	server debugServer // resolved debug server
}

// debugServer is a debug server that can be attached to the Envoy process.
type debugServer interface {
	// start attaches the debug server to the Envoy process and returns once it accepts connections.
	start(runCtx, ctx context.Context, logger *slog.Logger, envoyPid int) error
	// stop stops the debug server, detaching it from Envoy without killing it.
	stop(logger *slog.Logger)
	// connectHint returns how to connect to the debug server from the command line.
	connectHint() string
}

// resolve finds (or builds) the debug server to use. It is called before starting Envoy to fail fast if the
// debug server is not available.
func (d *DebugOptions) resolve(ctx context.Context, logger *slog.Logger) error {
	var err error
	switch d.Debugger {
	case DebuggerGDBRemote:
		d.server, err = newGDBRemoteServer(d, logger)
	default:
		d.server, err = newDelveServer(ctx, d, logger)
	}
	return err
}

// listenHost returns the host the debug server listens on.
func listenHost() string {
	return cmp.Or(os.Getenv(DebugListenHostEnv), "127.0.0.1")
}

// attach finds the Envoy process started by this process and attaches the debug server to it. The debug
// server is bound to runCtx so it is stopped when the run finishes.
func (d *DebugOptions) attach(runCtx, ctx context.Context, logger *slog.Logger) error {
	findCtx, cancel := context.WithTimeout(ctx, envoyProcessTimeout)
	defer cancel()
	pid, err := findEnvoyPid(findCtx, os.Getpid())
	if err != nil {
		return fmt.Errorf("failed to find the Envoy process to attach the debugger to: %w", err)
	}
	return d.server.start(runCtx, ctx, logger, pid)
}

// stop stops the debug server, if it was started.
func (d *DebugOptions) stop(logger *slog.Logger) {
	if d.server != nil {
		d.server.stop(logger)
	}
}

// printNotes prints how to connect to the debug server and the additional notes.
func (d *DebugOptions) printNotes() {
	_, _ = fmt.Fprintf(os.Stderr, "\n%s🐞 Connect your debugger to 127.0.0.1:%d%s\n  → %sCLI:%s %s\n\n",
		internal.ANSIBold, d.Port, internal.ANSIReset, internal.ANSIBold, internal.ANSIReset, d.server.connectHint())
	for _, note := range d.Notes {
		_, _ = fmt.Fprintln(os.Stderr, note)
	}
}

// startDebugServer starts a debug server process. Its output is prefixed with the given name.
func startDebugServer(ctx context.Context, logger *slog.Logger, name, path string, args []string) (*exec.Cmd, error) {
	// #nosec G204
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = &prefixedWriter{prefix: "[" + name + "] ", w: os.Stderr}
	cmd.Stderr = &prefixedWriter{prefix: "[" + name + "] ", w: os.Stderr}
	// Debug servers detach from Envoy (without killing it) when they receive SIGTERM.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	logger.Debug("starting debug server", "cmd", cmd.String())
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start %s: %w", name, err)
	}
	return cmd, nil
}

// stopDebugServer stops a debug server, which detaches it from Envoy without killing it.
func stopDebugServer(logger *slog.Logger, cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	logger.Debug("stopping debug server", "pid", cmd.Process.Pid)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	_ = cmd.Wait()
}

// DebugNotesFromEnv returns the debug notes passed in the environment by the host boe process.
func DebugNotesFromEnv() []string {
	if v := os.Getenv(debugNotesEnv); v != "" {
		return strings.Split(v, debugNotesSeparator)
	}
	return nil
}

// findExecutable checks that the executable exists at the given path.
func findExecutable(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s not found: %w", path, err)
	}
	return path, nil
}

// findEnvoyPid polls the children of the given process until it finds the Envoy process started by func-e.
// Envoy is identified by the --use-dynamic-base-id flag that RunnerFuncE always passes, which distinguishes
// it from other child processes such as ext_proc servers.
func findEnvoyPid(ctx context.Context, parentPid int) (int, error) {
	parent, err := process.NewProcessWithContext(ctx, int32(parentPid)) //nolint:gosec // pid never overflows int32
	if err != nil {
		return 0, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		children, _ := parent.ChildrenWithContext(ctx)
		for _, child := range children {
			cmdline, err := child.CmdlineSliceWithContext(ctx)
			if err == nil && slices.Contains(cmdline, "--use-dynamic-base-id") {
				return int(child.Pid), nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}
