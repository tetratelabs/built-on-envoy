// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestMergeGODEBUG(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		add      []string
		want     string
	}{
		{"empty", "", []string{"cgocheck=0"}, "cgocheck=0"},
		{"keeps existing", "madvdontneed=1", []string{"cgocheck=0"}, "madvdontneed=1,cgocheck=0"},
		{
			"overrides existing", "cgocheck=1,madvdontneed=1",
			[]string{"cgocheck=0", "asyncpreemptoff=1"},
			"madvdontneed=1,cgocheck=0,asyncpreemptoff=1",
		},
		{"ignores empty entries", ",madvdontneed=1,", []string{"cgocheck=0"}, "madvdontneed=1,cgocheck=0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeGODEBUG(tt.existing, tt.add...))
		})
	}
}

func TestDelveArgs(t *testing.T) {
	s := &delveServer{port: 40000}
	want := []string{
		"attach", "1234", "--headless", "--listen=127.0.0.1:40000",
		"--api-version=2", "--accept-multiclient", "--continue",
	}
	require.Equal(t, want, s.args(1234))

	t.Setenv(DebugListenHostEnv, "0.0.0.0")
	require.Contains(t, s.args(1234), "--listen=0.0.0.0:40000")
}

func TestPatchDelve(t *testing.T) {
	// Apply the patches to the actual Delve sources of the pinned version.
	out, err := exec.Command("go", "mod", "download", "-json", delveModule+"@"+DelveVersion).Output()
	if err != nil {
		t.Skipf("could not download Delve sources: %v", err)
	}
	var mod struct{ Dir string }
	require.NoError(t, json.Unmarshal(out, &mod))
	src := t.TempDir()
	require.NoError(t, os.CopyFS(src, os.DirFS(mod.Dir)))

	require.NoError(t, patchDelve(src))
	read := func(file string) string {
		content, err := os.ReadFile(filepath.Clean(filepath.Join(src, file)))
		require.NoError(t, err)
		return string(content)
	}
	require.Contains(t, read("pkg/proc/goroutine_cache.go"), "for _, image := range bi.Images {")
	require.Contains(t, read("pkg/proc/target.go"), "func (t *Target) InitGoImage() {")
	require.Contains(t, read("pkg/proc/native/proc_linux.go"), "sel.InitGoImage()")
	require.Contains(t, read("pkg/proc/bininfo.go"), "func (bi *BinaryInfo) setGStructOffsetElfSharedLib(")

	// The patched sources must compile on the platforms boe debug supports.
	for _, goarch := range []string{"amd64", "arm64"} {
		// #nosec G204
		cmd := exec.Command("go", "build", "-o", os.DevNull, "./cmd/dlv")
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0", "GOFLAGS=-mod=mod")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "linux/%s: %s", goarch, out)
	}

	// Patching twice fails, as the sources no longer match the patches.
	require.ErrorContains(t, patchDelve(src), "failed to apply Delve patch 0001")
}

func TestInstallDelveCached(t *testing.T) {
	// A cached binary is reused without building it.
	dir := t.TempDir()
	cached := filepath.Join(dir, "dlv-"+DelveVersion)
	require.NoError(t, os.WriteFile(cached, []byte("cached"), 0o600))
	got, err := installDelve(t.Context(), internaltesting.NewTLogger(t), dir, false)
	require.NoError(t, err)
	require.Equal(t, cached, got)

	// When rebuilding, the cached binary is not reused. Fail the build early with an invalid toolchain
	// so the test does not need to build Delve.
	t.Setenv("GOTOOLCHAIN", "invalid")
	_, err = installDelve(t.Context(), internaltesting.NewTLogger(t), dir, true)
	require.Error(t, err)
}

func TestNewDelveServer(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

	t.Run("explicit path", func(t *testing.T) {
		dlv := filepath.Join(t.TempDir(), "dlv")
		require.NoError(t, os.WriteFile(dlv, nil, 0o600))
		s, err := newDelveServer(t.Context(), &DebugOptions{ServerPath: dlv, Port: 40000}, logger)
		require.NoError(t, err)
		require.Equal(t, dlv, s.path)
		require.Equal(t, "dlv connect 127.0.0.1:40000", s.connectHint())
	})

	t.Run("explicit path not found", func(t *testing.T) {
		_, err := newDelveServer(t.Context(), &DebugOptions{ServerPath: filepath.Join(t.TempDir(), "dlv")}, logger)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("cached patched delve", func(t *testing.T) {
		dir := t.TempDir()
		cached := filepath.Join(dir, "dlv-"+DelveVersion)
		require.NoError(t, os.WriteFile(cached, nil, 0o600))
		s, err := newDelveServer(t.Context(), &DebugOptions{InstallDir: dir}, logger)
		require.NoError(t, err)
		require.Equal(t, cached, s.path)
	})
}

func TestDelveServerStartStop(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dlv := filepath.Join(t.TempDir(), "dlv")
	require.NoError(t, os.WriteFile(dlv, []byte(fakeDelve), 0o700)) //nolint:gosec // test executable

	port := internaltesting.FreePorts(t, 1)[0]
	s := &delveServer{path: dlv, port: uint32(port)} //nolint:gosec // port fits in uint32
	logger := internaltesting.NewTLogger(t)
	require.NoError(t, s.start(t.Context(), t.Context(), logger, 4321))

	// The server accepts connections once started, and is stopped (and waited for) by stop.
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	s.stop(logger)
	require.NotNil(t, s.cmd.ProcessState, "the debug server process must have exited")
}

func TestDelveServerStartNotReady(t *testing.T) {
	// A dlv that exits without listening never becomes ready.
	dlv := filepath.Join(t.TempDir(), "dlv")
	require.NoError(t, os.WriteFile(dlv, []byte("#!/bin/sh\nexit 0\n"), 0o700))    //nolint:gosec // test executable
	s := &delveServer{path: dlv, port: uint32(internaltesting.FreePorts(t, 1)[0])} //nolint:gosec // port fits in uint32
	orig := debugServerReadyTimeout
	debugServerReadyTimeout = time.Second
	t.Cleanup(func() { debugServerReadyTimeout = orig })
	require.ErrorContains(t, s.start(t.Context(), t.Context(), internaltesting.NewTLogger(t), 4321), "delve did not become ready")
}

func TestInstallDelve(t *testing.T) {
	// Build the patched Delve for the current platform.
	if _, err := exec.Command("go", "mod", "download", delveModule+"@"+DelveVersion).CombinedOutput(); err != nil {
		t.Skipf("could not download Delve sources: %v", err)
	}
	dir := t.TempDir()
	dlv, err := installDelve(t.Context(), internaltesting.NewTLogger(t), dir, false)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "dlv-"+DelveVersion), dlv)
	out, err := exec.Command(dlv, "version").CombinedOutput() //nolint:gosec // test command
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "Delve Debugger")
}

// fakeDelve is a fake dlv that listens on the address given in the --listen flag until it is terminated.
const fakeDelve = `#!/usr/bin/env python3
import signal, socket, sys
addr = next(a for a in sys.argv if a.startswith("--listen=")).split("=", 1)[1]
host, port = addr.rsplit(":", 1)
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
l = socket.socket(); l.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); l.bind((host, int(port))); l.listen(1)
while True:
    l.accept()[0].close()
`
