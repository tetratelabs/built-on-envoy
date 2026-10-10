// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"bytes"
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
	"strconv"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
)

const (
	// DelveVersion is the version of Delve built by boe.
	DelveVersion = "v1.27.1"
	// delveModule is the Go module of Delve.
	delveModule = "github.com/go-delve/delve"
)

// delveServer is a headless Delve server, used to debug Go extensions.
type delveServer struct {
	path string
	port uint32
	cmd  *exec.Cmd
}

// newDelveServer returns a Delve server. Unless a dlv binary is explicitly configured, boe builds its own
// patched Delve (see delvePatches), as upstream Delve cannot properly debug a Go runtime that lives in a
// shared library loaded by a non-Go process (such as Envoy).
func newDelveServer(ctx context.Context, d *DebugOptions, logger *slog.Logger) (*delveServer, error) {
	var (
		dlvPath string
		err     error
	)
	if d.ServerPath != "" {
		_, _ = fmt.Fprintf(os.Stderr, "%s⚠ Using %s: unpatched Delve versions may not be able to list goroutines.%s\n",
			internal.ANSIBold, d.ServerPath, internal.ANSIReset)
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

func (s *delveServer) start(runCtx, _ context.Context, logger *slog.Logger, pid int) error {
	_, _ = fmt.Fprintf(os.Stderr, "🐞 %sAttaching Delve to Envoy (PID %d)...%s\n", internal.ANSIBold, pid, internal.ANSIReset)
	var err error
	if s.cmd, err = startDebugServer(runCtx, logger, "dlv", s.path, s.args(pid)); err != nil {
		return err
	}
	if err = waitForTCPReady(net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(s.port), 10)), debugServerReadyTimeout); err != nil {
		stopDebugServer(logger, s.cmd)
		return fmt.Errorf("delve did not become ready: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s🐞 Delve is attached to Envoy and listening on 127.0.0.1:%d%s (dlv PID %d)\n",
		internal.ANSIBold, s.port, internal.ANSIReset, s.cmd.Process.Pid)
	return nil
}

func (s *delveServer) stop(logger *slog.Logger) { stopDebugServer(logger, s.cmd) }

func (s *delveServer) connectHint() string { return fmt.Sprintf("dlv connect 127.0.0.1:%d", s.port) }

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
