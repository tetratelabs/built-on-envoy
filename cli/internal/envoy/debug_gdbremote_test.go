// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
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

func TestGDBRemoteServerArgs(t *testing.T) {
	linux := &gdbRemoteServer{goos: "linux"}
	require.Equal(t, []string{"--once", "--attach", "127.0.0.1:5000", "1234"}, linux.args(1234, "127.0.0.1:5000"))
	require.Equal(t, "gdb -ex 'target remote 127.0.0.1:0'", linux.connectHint())
	linux.envoyPath = "/path/to/envoy"
	require.Contains(t, linux.connectHint(), `"program": "/path/to/envoy"`)
	require.NotContains(t, linux.connectHint(), "lldb")

	darwin := &gdbRemoteServer{goos: "darwin", port: 2345}
	require.Equal(t, []string{"127.0.0.1:5000", "--attach=1234"}, darwin.args(1234, "127.0.0.1:5000"))
	require.Equal(t, "lldb -o 'gdb-remote 127.0.0.1:2345'", darwin.connectHint())
}

func TestProcessAlive(t *testing.T) {
	require.True(t, processAlive(os.Getpid()))

	// An exited process that has not been waited for yet is a zombie: it is not alive.
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() })
	require.Eventually(t, func() bool { return !processAlive(cmd.Process.Pid) }, 10*time.Second, 50*time.Millisecond)

	require.NoError(t, cmd.Wait())
	require.False(t, processAlive(cmd.Process.Pid))
}

func TestNewGDBRemoteServer(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

	t.Run("explicit path", func(t *testing.T) {
		bin := filepath.Join(t.TempDir(), "gdbserver")
		require.NoError(t, os.WriteFile(bin, nil, 0o600))
		s, err := newGDBRemoteServer(&DebugOptions{ServerPath: bin}, logger)
		require.NoError(t, err)
		require.Equal(t, bin, s.path)
	})

	t.Run("explicit path not found", func(t *testing.T) {
		_, err := newGDBRemoteServer(&DebugOptions{ServerPath: filepath.Join(t.TempDir(), "gdbserver")}, logger)
		require.ErrorContains(t, err, "not found")
	})
}

func TestGDBRemoteServerSessions(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	script := filepath.Join(t.TempDir(), "gdbserver")
	require.NoError(t, os.WriteFile(script, []byte(fakeGDBServer), 0o700)) //nolint:gosec // test executable

	port := internaltesting.FreePorts(t, 1)[0]
	s := &gdbRemoteServer{goos: "linux", path: script, port: uint32(port)} //nolint:gosec // port fits in uint32
	logger := internaltesting.NewTLogger(t)
	// Sessions go on while the "Envoy" process is alive: use a child process that keeps running.
	envoy := exec.Command("sleep", "60")
	require.NoError(t, envoy.Start())
	t.Cleanup(func() { _ = envoy.Process.Kill(); _ = envoy.Wait() })
	pid := envoy.Process.Pid
	require.NoError(t, s.start(t.Context(), t.Context(), logger, pid))
	t.Cleanup(func() { s.stop(logger) })

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	// session connects a debugger, calls during before disconnecting, and returns once disconnected.
	session := func(during func()) {
		conn, err := net.Dial("tcp", addr)
		require.NoError(t, err)
		_, err = conn.Write([]byte("ping"))
		require.NoError(t, err)
		buf := make([]byte, 32)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(30*time.Second)))
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.Equal(t, "pong "+strconv.Itoa(pid), string(buf[:n]))
		during()
		require.NoError(t, conn.Close())
	}

	// Each connection attaches a new debug server, so clients can reconnect.
	session(func() {})
	session(func() {})

	// When Envoy does not survive a session (the debug server kills it when the debugger quits without
	// detaching), no more debuggers are accepted.
	session(func() {
		require.NoError(t, envoy.Process.Kill())
		_ = envoy.Wait()
	})
	require.Eventually(t, func() bool {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return true
		}
		// The listener is still open, but nobody serves the connection: the read times out.
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write([]byte("ping"))
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, err = conn.Read(make([]byte, 32))
		return err != nil
	}, 10*time.Second, 100*time.Millisecond)
}

// fakeGDBServer is a fake gdbserver: it serves a single connection on the address given as the 3rd argument
// (as in "--once --attach <addr> <pid>"), replying with the pid it was attached to, then exits.
const fakeGDBServer = `#!/usr/bin/env python3
import socket, sys
host, port = sys.argv[3].rsplit(":", 1)
l = socket.socket(); l.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); l.bind((host, int(port))); l.listen(1)
while True:
    c, _ = l.accept()
    if c.recv(16) == b"ping":
        c.sendall(("pong " + sys.argv[4]).encode())
        c.recv(16)  # wait for the client to disconnect
        sys.exit(0)
    c.close()  # readiness probe
`
