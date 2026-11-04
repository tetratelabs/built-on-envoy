// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
)

const (
	// DelveVersion is the version of Delve built by boe. Debugging a Go runtime that lives in a shared
	// library loaded by a non-Go process (libcomposer.so in Envoy) after attaching to it requires
	// https://github.com/go-delve/delve/pull/4456, not yet in a Delve release.
	DelveVersion = "v1.27.3-0.20261104171615-749588dac987"
	// delveModule is the Go module of Delve.
	delveModule = "github.com/go-delve/delve"
)

// delveServer is a headless Delve server, used to debug Go extensions.
type delveServer struct {
	path   string
	port   uint32
	cmd    *exec.Cmd
	exited chan struct{} // closed when the dlv process exits
}

// newDelveServer returns a Delve server. Unless a dlv binary is explicitly configured, boe builds Delve
// DelveVersion, as older versions cannot properly debug a Go runtime that lives in a shared library loaded
// by a non-Go process (such as Envoy).
func newDelveServer(ctx context.Context, d *DebugOptions, logger *slog.Logger) (*delveServer, error) {
	var (
		dlvPath string
		err     error
	)
	if d.ServerPath != "" {
		_, _ = fmt.Fprintf(os.Stderr, "%s⚠ Using %s: Delve versions older than %s may not be able to list goroutines.%s\n",
			internal.ANSIBold, d.ServerPath, DelveVersion, internal.ANSIReset)
		dlvPath, err = findExecutable(d.ServerPath)
	} else {
		dlvPath, err = installDelve(ctx, logger, d.InstallDir, d.Rebuild)
	}
	if err != nil {
		return nil, err
	}
	logger.Debug("using delve", "path", dlvPath)
	return &delveServer{path: dlvPath, port: d.Port}, nil
}

// args returns the arguments to start a headless Delve server attached to the given process.
func (s *delveServer) args(pid int) []string {
	return []string{
		"attach", strconv.Itoa(pid),
		"--headless",
		"--listen=" + net.JoinHostPort(listenHost(), strconv.FormatUint(uint64(s.port), 10)),
		"--api-version=2",
		"--accept-multiclient",
		"--continue", // Do not stop Envoy when attaching; it keeps serving until a breakpoint is hit.
	}
}

func (s *delveServer) start(runCtx, ctx context.Context, logger *slog.Logger, pid int) error {
	_, _ = fmt.Fprintf(os.Stderr, "🐞 %sAttaching Delve to Envoy (PID %d)...%s\n", internal.ANSIBold, pid, internal.ANSIReset)
	var err error
	if s.cmd, err = startDebugServer(runCtx, logger, "dlv", s.path, s.args(pid)); err != nil {
		return err
	}
	s.exited = make(chan struct{})
	go func() {
		_ = s.cmd.Wait() // the only Wait call for this process
		close(s.exited)
	}()
	// Stop waiting as soon as dlv exits (e.g. when it is not allowed to attach to Envoy) instead of waiting
	// for the full timeout.
	conn, err := dialWithRetry(ctx, net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(s.port), 10)), debugServerReadyTimeout, s.exited)
	if err != nil {
		s.stop(logger)
		return fmt.Errorf("delve did not become ready: %w", err)
	}
	_ = conn.Close()
	_, _ = fmt.Fprintf(os.Stderr, "%s🐞 Delve is attached to Envoy and listening on 127.0.0.1:%d%s (dlv PID %d)\n",
		internal.ANSIBold, s.port, internal.ANSIReset, s.cmd.Process.Pid)
	return nil
}

func (s *delveServer) stop(logger *slog.Logger) {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	logger.Debug("stopping debug server", "pid", s.cmd.Process.Pid)
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	<-s.exited
}

func (s *delveServer) connectHint() string { return fmt.Sprintf("dlv connect 127.0.0.1:%d", s.port) }

// installDelve builds Delve in the given directory, unless it is already there and rebuild is false.
func installDelve(ctx context.Context, logger *slog.Logger, dir string, rebuild bool) (string, error) {
	dlvPath := filepath.Join(dir, "dlv-"+DelveVersion)
	if _, err := os.Stat(dlvPath); err == nil && !rebuild {
		return dlvPath, nil
	}
	_, _ = fmt.Fprintf(os.Stderr, "→ %sBuilding Delve %s...%s\n", internal.ANSIBold, DelveVersion, internal.ANSIReset)

	// Install Delve in a temporary directory and move it to its versioned path, so that a failed build
	// does not leave a broken binary in the cache. The temporary directory is in the same directory, so
	// that the binary can be renamed.
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	bin, err := os.MkdirTemp(dir, "dlv-build-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(bin) }()
	// #nosec G204
	cmd := exec.CommandContext(ctx, "go", "install", delveModule+"/cmd/dlv@"+DelveVersion)
	cmd.Dir = os.TempDir() // Not in a module, so the build is not affected by any go.mod
	cmd.Env = append(os.Environ(), "GOBIN="+bin, "CGO_ENABLED=0")
	logger.Debug("building delve", "cmd", cmd.String())
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("failed to build Delve: %w\nOutput: %s", buildErr, out)
	}

	if err = os.Rename(filepath.Join(bin, "dlv"), dlvPath); err != nil {
		return "", fmt.Errorf("failed to install Delve: %w", err)
	}
	return dlvPath, nil
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
