// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
)

const (
	// DefaultDelvePort is the default port for the headless Delve server.
	DefaultDelvePort = 2345
	// DelveVersion is the version of Delve built by boe.
	DelveVersion = "v1.27.1"
	// delveModule is the Go module of Delve.
	delveModule = "github.com/go-delve/delve"
	// DelveListenHostEnv overrides the address the headless Delve server listens on. Set by RunnerDocker
	// so Delve listens on all interfaces inside the container.
	DelveListenHostEnv = "BOE_DLV_LISTEN_HOST"
	// debugNotesEnv passes the debug notes computed in the host to the boe process running in the container.
	debugNotesEnv = "BOE_DEBUG_NOTES"
	// debugNotesSeparator separates the debug notes in debugNotesEnv.
	debugNotesSeparator = "\x1e"

	// envoyProcessTimeout is how long to wait for the Envoy process to be found.
	envoyProcessTimeout = 30 * time.Second
	// delveReadyTimeout is how long to wait for the Delve server to accept connections.
	delveReadyTimeout = 60 * time.Second
)

// DebugOptions configures attaching a debugger to the Envoy process.
type DebugOptions struct {
	// DelvePath is the path to the dlv binary. If empty, boe builds a patched Delve in InstallDir.
	DelvePath string
	// InstallDir is the directory where boe builds Delve.
	InstallDir string
	// Rebuild forces rebuilding Delve even if it is already in InstallDir.
	Rebuild bool
	// DelvePort is the port the headless Delve server listens on.
	DelvePort uint32
	// Notes are additional messages printed after the instructions to connect to Delve.
	Notes []string

	dlvPath string // resolved path to the dlv binary
}

// resolveDelve returns the dlv binary to use. Unless a dlv binary is explicitly configured, boe builds
// its own patched Delve (see patchDelve), as upstream Delve cannot list the goroutines of a Go runtime that
// lives in a shared library loaded by a non-Go process (such as Envoy).
func (d *DebugOptions) resolveDelve(ctx context.Context, logger *slog.Logger) error {
	var (
		dlvPath string
		err     error
	)
	if d.DelvePath != "" {
		_, _ = fmt.Fprintf(os.Stderr, "%s⚠ Using %s: unpatched Delve versions may not be able to list goroutines.%s\n",
			internal.ANSIBold, d.DelvePath, internal.ANSIReset)
		dlvPath, err = findDelve(d.DelvePath)
	} else {
		dlvPath, err = installDelve(ctx, logger, d.InstallDir, d.Rebuild)
	}
	if err != nil {
		return err
	}
	logger.Debug("using delve", "path", dlvPath)
	d.dlvPath = dlvPath
	return nil
}

// listenAddress returns the address the headless Delve server listens on.
func (d *DebugOptions) listenAddress() string {
	host := cmp.Or(os.Getenv(DelveListenHostEnv), "127.0.0.1")
	return net.JoinHostPort(host, strconv.FormatUint(uint64(d.DelvePort), 10))
}

// delveArgs returns the arguments to start a headless Delve server attached to the given process.
func (d *DebugOptions) delveArgs(pid int) []string {
	return []string{
		"attach", strconv.Itoa(pid),
		"--headless",
		"--listen=" + d.listenAddress(),
		"--api-version=2",
		"--accept-multiclient",
		"--continue", // Do not stop Envoy when attaching; it keeps serving until a breakpoint is hit.
	}
}

// attach finds the Envoy process started by this process and attaches a headless Delve server to it.
// The Delve process is bound to runCtx so it is stopped when the run finishes.
func (d *DebugOptions) attach(runCtx, ctx context.Context, logger *slog.Logger) (*exec.Cmd, error) {
	findCtx, cancel := context.WithTimeout(ctx, envoyProcessTimeout)
	defer cancel()
	pid, err := findEnvoyPid(findCtx, os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("failed to find the Envoy process to attach the debugger to: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "🐞 %sAttaching Delve to Envoy (PID %d)...%s\n", internal.ANSIBold, pid, internal.ANSIReset)

	// #nosec G204
	cmd := exec.CommandContext(runCtx, d.dlvPath, d.delveArgs(pid)...)
	cmd.Stdout = &prefixedWriter{prefix: "[dlv] ", w: os.Stderr}
	cmd.Stderr = &prefixedWriter{prefix: "[dlv] ", w: os.Stderr}
	// Delve detaches from Envoy (without killing it) when it receives SIGINT or SIGTERM.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	logger.Debug("starting delve", "cmd", cmd.String())
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Delve: %w", err)
	}

	if err = waitForTCPReady(net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(d.DelvePort), 10)), delveReadyTimeout); err != nil {
		stopDelve(logger, cmd)
		return nil, fmt.Errorf("delve did not become ready: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "%s🐞 Delve is attached to Envoy and listening on 127.0.0.1:%d%s (dlv PID %d)\n",
		internal.ANSIBold, d.DelvePort, internal.ANSIReset, cmd.Process.Pid)

	return cmd, nil
}

// DebugNotesFromEnv returns the debug notes passed in the environment by the host boe process.
func DebugNotesFromEnv() []string {
	if v := os.Getenv(debugNotesEnv); v != "" {
		return strings.Split(v, debugNotesSeparator)
	}
	return nil
}

// stopDelve stops the Delve server, which detaches it from Envoy without killing it.
func stopDelve(logger *slog.Logger, cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	logger.Debug("stopping delve", "pid", cmd.Process.Pid)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	_ = cmd.Wait()
}

// findDelve checks that the dlv binary exists at the given path.
func findDelve(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("dlv not found at %s: %w", path, err)
	}
	return path, nil
}

// installDelve builds the patched Delve in the given directory, unless it is already there and rebuild is false.
func installDelve(ctx context.Context, logger *slog.Logger, dir string, rebuild bool) (string, error) {
	dlvPath := filepath.Join(dir, "dlv-"+DelveVersion)
	if _, err := os.Stat(dlvPath); err == nil && !rebuild {
		return dlvPath, nil
	}
	_, _ = fmt.Fprintf(os.Stderr, "→ %sBuilding Delve %s...%s\n", internal.ANSIBold, DelveVersion, internal.ANSIReset)

	// Download the Delve sources to the module cache and copy them to a writable directory to patch them.
	// #nosec G204
	cmd := exec.CommandContext(ctx, "go", "mod", "download", "-json", delveModule+"@"+DelveVersion)
	cmd.Dir = os.TempDir() // Not in a module, so the download is not affected by any go.mod
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to download Delve sources: %w\nOutput: %s", err, out)
	}
	var mod struct{ Dir string }
	if err = json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		return "", fmt.Errorf("failed to download Delve sources: unexpected output: %s", out)
	}
	src, err := os.MkdirTemp("", "boe-delve-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(src) }()
	if err = os.CopyFS(src, os.DirFS(mod.Dir)); err != nil {
		return "", fmt.Errorf("failed to copy Delve sources: %w", err)
	}
	if err = patchDelve(src); err != nil {
		return "", err
	}

	if err = os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	// #nosec G204
	cmd = exec.CommandContext(ctx, "go", "build", "-o", dlvPath, "./cmd/dlv")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod")
	logger.Debug("building delve", "cmd", cmd.String(), "dir", src)
	if out, err = cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build Delve: %w\nOutput: %s", err, out)
	}
	return dlvPath, nil
}

// delvePatches contains the patches applied to Delve, in a directory named after the Delve version
// they apply to (DelveVersion). The patches are applied in lexical order. They fix debugging a Go
// runtime that lives in a shared library loaded by a non-Go process (libcomposer.so in Envoy) after
// attaching to it. See the description in each patch file.
//
//go:embed delve
var delvePatches embed.FS

// patchDelve applies the patches for DelveVersion to the Delve sources in the given directory.
func patchDelve(src string) error {
	patches, err := fs.Glob(delvePatches, path.Join("delve", DelveVersion, "*.patch"))
	if err != nil || len(patches) == 0 {
		return fmt.Errorf("no Delve patches found for %s", DelveVersion)
	}
	for _, p := range patches { // fs.Glob returns the files in lexical order
		if err = applyPatch(src, p); err != nil {
			return fmt.Errorf("failed to apply Delve patch %s: %w", path.Base(p), err)
		}
	}
	return nil
}

// applyPatch applies the given embedded patch file to the sources in the given directory.
func applyPatch(src, patch string) error {
	content, err := delvePatches.ReadFile(patch)
	if err != nil {
		return err
	}
	files, _, err := gitdiff.Parse(bytes.NewReader(content))
	if err != nil {
		return err
	}
	for _, f := range files {
		file := filepath.Join(src, filepath.FromSlash(f.OldName))
		original, err := os.ReadFile(file) //nolint:gosec // path built from a temp dir and an embedded patch
		if err != nil {
			return err
		}
		var patched bytes.Buffer
		if err = gitdiff.Apply(&patched, bytes.NewReader(original), f); err != nil {
			return fmt.Errorf("%s: %w", f.OldName, err)
		}
		if err = os.WriteFile(file, patched.Bytes(), 0o600); err != nil {
			return err
		}
	}
	return nil
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

// mergeGODEBUG merges the given settings into an existing GODEBUG value. Settings in add override
// existing settings with the same key.
func mergeGODEBUG(existing string, add ...string) string {
	keys := make(map[string]bool, len(add))
	for _, a := range add {
		k, _, _ := strings.Cut(a, "=")
		keys[k] = true
	}
	var merged []string
	for _, e := range strings.Split(existing, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if k, _, _ := strings.Cut(e, "="); keys[k] {
			continue
		}
		merged = append(merged, e)
	}
	return strings.Join(append(merged, add...), ",")
}
