// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestHeaderCase(t *testing.T) {
	skipBeforeEnvoy140(t)

	tests := []struct {
		name   string
		config string
		// Header lines expected on each side, exactly as written on the wire.
		upstreamSees []string
		clientSees   []string
	}{
		{
			name:   "preserve with override",
			config: `{"mode":"preserve","overrides":{"content-type":"Content-type"}}`,
			upstreamSees: []string{
				"X-Client-MiXeD: 1",
				"Content-type: text/plain",
				// Added by Envoy, so it falls back to proper case.
				"X-Request-Id: ",
			},
			clientSees: []string{
				"X-Legacy-ID: 42",
				"Content-type: text/plain",
				"Server: envoy",
			},
		},
		{
			name:         "proper case",
			config:       `{"mode":"proper_case"}`,
			upstreamSees: []string{"X-Client-Mixed: 1", "Content-Type: text/plain"},
			clientSees:   []string{"X-Legacy-Id: 42", "Content-Type: text/plain"},
		},
		{
			name:         "lowercase",
			config:       `{"mode":"lowercase"}`,
			upstreamSees: []string{"x-client-mixed: 1", "content-type: text/plain"},
			clientSees:   []string{"x-legacy-id: 42", "content-type: text/plain"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstreamAddr, requests := startRawUpstream(t)
			// go-httpbin can't be used here: Go's net/http canonicalizes header names.
			t.Setenv(internaltesting.TestUpstreamClusterInsecure.Name(), upstreamAddr)

			ports := internaltesting.FreePorts(t, 2)
			proxyPort, adminPort := ports[0], ports[1]
			internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
				"--local", "../../header-case",
				"--config", tc.config)

			var response string
			require.Eventually(t, func() bool {
				var err error
				response, err = rawGet(proxyPort)
				return err == nil && strings.HasPrefix(response, "HTTP/1.1 200")
			}, 30*time.Second, 200*time.Millisecond)

			var request string
			select {
			case request = <-requests:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream did not receive the request")
			}

			for _, line := range tc.upstreamSees {
				assert.Contains(t, request, "\r\n"+line, "upstream request:\n%s", request)
			}
			for _, line := range tc.clientSees {
				assert.Contains(t, response, "\r\n"+line, "client response:\n%s", response)
			}
		})
	}
}

// skipBeforeEnvoy140 skips the test when ENVOY_VERSION pins an Envoy without the dynamic module
// header formatter, which first shipped in 1.40.0. Unset or dev versions run the test.
func skipBeforeEnvoy140(t *testing.T) {
	v := os.Getenv("ENVOY_VERSION")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 == nil && err2 == nil && (major < 1 || (major == 1 && minor < 40)) {
		t.Skipf("header formatter dynamic modules need Envoy 1.40+, got %s", v)
	}
}

// startRawUpstream starts an HTTP/1 server that records each request head as received and replies
// with mixed-case response headers.
func startRawUpstream(t *testing.T) (string, <-chan string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	requests := make(chan string, 16)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go serveRaw(conn, requests)
		}
	}()
	return l.Addr().String(), requests
}

func serveRaw(conn net.Conn, requests chan<- string) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		head, err := readHead(r)
		if err != nil {
			return
		}
		select {
		case requests <- head:
		default:
		}
		_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\n"+
			"content-TYPE: text/plain\r\n"+
			"X-Legacy-ID: 42\r\n"+
			"Content-Length: 2\r\n\r\nok")
		if err != nil {
			return
		}
	}
}

// rawGet sends a request with mixed-case headers and returns the raw response head.
func rawGet(port int) (string, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\n"+
		"Host: test\r\n"+
		"X-Client-MiXeD: 1\r\n"+
		"CONTENT-TYPE: text/plain\r\n"+
		"Connection: close\r\n\r\n")
	if err != nil {
		return "", err
	}
	return readHead(bufio.NewReader(conn))
}

// readHead reads the start line and header lines, keeping the bytes exactly as sent.
func readHead(r *bufio.Reader) (string, error) {
	tp := textproto.NewReader(r)
	var b strings.Builder
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return "", err
		}
		if line == "" {
			return b.String(), nil
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}
}
