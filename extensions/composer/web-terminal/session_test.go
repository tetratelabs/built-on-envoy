// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"
)

// TestRunSessionEchoAndResize drives runSession over a real loopback socket with
// a real gobwas WebSocket client and a `cat` PTY, so input is echoed back.
func TestRunSessionEchoAndResize(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			runSession(c, &config{Command: "cat"}, nil, nil)
		}
	}()

	conn, _, _, err := ws.Dial(context.Background(), "ws://"+ln.Addr().String()+"/")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	require.NoError(t, wsutil.WriteClientBinary(conn, append([]byte{opResize}, []byte(`{"cols":100,"rows":40}`)...)))
	require.NoError(t, wsutil.WriteClientBinary(conn, append([]byte{opInput}, []byte("ping-123\n")...)))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var out []byte
	for !bytes.Contains(out, []byte("ping-123")) {
		msg, _, rerr := wsutil.ReadServerData(conn)
		require.NoError(t, rerr)
		out = append(out, msg...)
	}
}

// TestRunSessionFrontendWebSocket: with serve_frontend on (the buffered/peek
// path), a WebSocket upgrade still opens the terminal.
func TestRunSessionFrontendWebSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		if c, aerr := ln.Accept(); aerr == nil {
			runSession(c, &config{Command: "cat", ServeFrontend: true}, nil, nil)
		}
	}()

	conn, _, _, err := ws.Dial(context.Background(), "ws://"+ln.Addr().String()+"/")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	require.NoError(t, wsutil.WriteClientBinary(conn, append([]byte{opInput}, []byte("buf-9\n")...)))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var out []byte
	for !bytes.Contains(out, []byte("buf-9")) {
		msg, _, rerr := wsutil.ReadServerData(conn)
		require.NoError(t, rerr)
		out = append(out, msg...)
	}
}

// TestRunSessionPTYStartError: after a successful upgrade, a bad command makes
// pty.Start fail, which is logged via logf.
func TestRunSessionPTYStartError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	logs := make(chan string, 4)
	go func() {
		if c, aerr := ln.Accept(); aerr == nil {
			runSession(c, &config{Command: "/nonexistent/boe-xyz"}, nil,
				func(_ shared.LogLevel, format string, _ ...any) { logs <- format })
		}
	}()

	conn, _, _, err := ws.Dial(context.Background(), "ws://"+ln.Addr().String()+"/")
	require.NoError(t, err) // handshake succeeds, then pty start fails
	defer func() { _ = conn.Close() }()

	select {
	case msg := <-logs:
		require.Contains(t, msg, "pty start")
	case <-time.After(3 * time.Second):
		t.Fatal("expected a pty start error to be logged")
	}
}

// TestRunSessionBadHandshake ensures a non-WebSocket request makes runSession
// return promptly instead of hanging.
func TestRunSessionBadHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	done := make(chan struct{})
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			runSession(c, &config{Command: "cat"}, nil, nil)
		}
		close(done)
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	_, _ = raw.Write([]byte("GET / HTTP/1.1\r\nhost: x\r\n\r\n")) // not an Upgrade
	_ = raw.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runSession did not return on a non-WebSocket request")
	}
}
