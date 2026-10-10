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
	require.Contains(t, linux.connectHint(), "gdb -ex 'target remote 127.0.0.1:")

	darwin := &gdbRemoteServer{goos: "darwin", port: 2345}
	require.Equal(t, []string{"127.0.0.1:5000", "--attach=1234"}, darwin.args(1234, "127.0.0.1:5000"))
	require.Equal(t, "lldb -o 'gdb-remote 127.0.0.1:2345'", darwin.connectHint())
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
	require.NoError(t, s.start(t.Context(), t.Context(), logger, 4321))
	t.Cleanup(func() { s.stop(logger) })

	// Each connection attaches a new debug server, so clients can reconnect.
	for range 2 {
		conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		require.NoError(t, err)
		_, err = conn.Write([]byte("ping"))
		require.NoError(t, err)
		buf := make([]byte, 32)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(30*time.Second)))
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.Equal(t, "pong 4321", string(buf[:n]))
		require.NoError(t, conn.Close())
	}
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
