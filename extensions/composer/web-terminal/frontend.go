// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"io"
)

// frontendHTML is the bundled browser client, served on plain GET when
// serve_frontend is set.
//
//go:embed frontend/index.html
var frontendHTML []byte

// maxRequestHead caps the request-classification peek.
const maxRequestHead = 8192

// bufConn reads from a buffered reader (so peeked bytes aren't lost) and writes
// to the underlying connection.
type bufConn struct {
	r *bufio.Reader
	w io.Writer
}

func (c bufConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c bufConn) Write(p []byte) (int, error) { return c.w.Write(p) }

// peekIsWebSocket reports whether the opening request is a WebSocket upgrade
// without consuming it, blocking only for header bytes still being sent.
func peekIsWebSocket(br *bufio.Reader) bool {
	for {
		if _, err := br.Peek(1); err != nil { // await the request
			return false
		}
		n := br.Buffered()
		b, _ := br.Peek(n) // buffered bytes only, no blocking
		if i := bytes.Index(b, []byte("\r\n\r\n")); i >= 0 {
			return headerIsUpgrade(b[:i])
		}
		if n >= maxRequestHead {
			return headerIsUpgrade(b)
		}
		if _, err := br.Peek(n + 1); err != nil { // pull more headers
			return headerIsUpgrade(b)
		}
	}
}

// headerIsUpgrade heuristically flags a WebSocket upgrade. gobwas validates
// strictly after.
func headerIsUpgrade(head []byte) bool {
	return bytes.Contains(bytes.ToLower(head), []byte("websocket"))
}

// serveFrontend writes the bundled client as a one-shot HTTP/1.1 response.
func serveFrontend(w io.Writer) {
	_, _ = fmt.Fprintf(w, "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n"+
		"Content-Length: %d\r\nConnection: close\r\n\r\n", len(frontendHTML))
	_, _ = w.Write(frontendHTML)
}
