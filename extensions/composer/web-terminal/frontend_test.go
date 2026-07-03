// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestServeFrontend: with serve_frontend on, a plain GET is answered with the
// bundled client page (no WebSocket, no PTY).
func TestServeFrontend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		if c, aerr := ln.Accept(); aerr == nil {
			runSession(c, &config{Command: "cat", ServeFrontend: true}, nil, nil)
			_ = c.Close() // mimic serve()'s closeConn so the client sees EOF
		}
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	_, err = c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.NoError(t, err)

	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	body, _ := io.ReadAll(c)
	resp := string(body)
	require.Contains(t, resp, "200 OK")
	require.Contains(t, resp, "text/html")
	require.Contains(t, resp, "<title>web-terminal</title>")
}

// TestServeFrontendDisabled: without serve_frontend, a plain GET gets nothing
// (the connection is just dropped).
func TestServeFrontendDisabled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		if c, aerr := ln.Accept(); aerr == nil {
			runSession(c, &config{Command: "cat", ServeFrontend: false}, nil, nil)
			_ = c.Close()
		}
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	_, err = c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.NoError(t, err)

	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	body, _ := io.ReadAll(c)
	require.NotContains(t, string(body), "200 OK")
}

func TestHeaderIsUpgrade(t *testing.T) {
	require.True(t, headerIsUpgrade([]byte("GET /ws HTTP/1.1\r\nUpgrade: websocket\r\n")))
	require.False(t, headerIsUpgrade([]byte("GET / HTTP/1.1\r\nHost: x\r\n")))
}
