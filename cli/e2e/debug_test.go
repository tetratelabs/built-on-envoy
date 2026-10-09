// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

// TestDebugLocalComposer runs a composer extension with `boe debug`, connects to the Delve server attached to
// Envoy (as an IDE would do), sets a breakpoint by file and line in the extension source, and verifies the
// breakpoint is hit when a request is sent to Envoy.
func TestDebugLocalComposer(t *testing.T) {
	internaltesting.MaybeSkipLongRunningTest(t)
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("dlv"); err != nil {
			t.Skip("dlv is required to run boe debug on Linux")
		}
	} else if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker is required to run boe debug on %s", runtime.GOOS)
	}

	extensionDir, err := filepath.Abs("../../extensions/composer/example")
	require.NoError(t, err)
	sourceFile := filepath.Join(extensionDir, "example.go")
	const sourceLine = 36 // First line in the body of example.(*Plugin).OnRequestHeaders

	ports := internaltesting.FreePorts(t, 3)
	proxyPort, adminPort, dlvPort := ports[0], ports[1], ports[2]
	internaltesting.RunBoe(t, cliBin, "debug", proxyPort, adminPort,
		"--dlv-port", strconv.Itoa(dlvPort),
		"--local", extensionDir,
	)

	// When running in a container the published ports accept connections before the services in the container
	// are ready, so wait until the Delve API answers, which happens after Envoy is started and Delve attached.
	dlvAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(dlvPort))
	var client *rpc.Client
	require.Eventually(t, func() bool {
		c, err := jsonrpc.Dial("tcp", dlvAddr)
		if err != nil {
			return false
		}
		var version getVersionOut
		if err = c.Call("RPCServer.GetVersion", struct{}{}, &version); err != nil {
			_ = c.Close()
			return false
		}
		client = c
		return true
	}, internaltesting.RunEnvoyTimeout.Get(), time.Second, "Delve API not available at %s", dlvAddr)
	t.Cleanup(func() { _ = client.Close() })

	// Envoy must be ready before sending requests to it.
	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/ready", adminPort), internaltesting.EqualStatus(http.StatusOK))

	// Set a breakpoint by file and line, as an IDE does. The source paths in the debug information must
	// match the host paths, even when Envoy runs in a container.
	var bp createBreakpointOut
	require.NoError(t, client.Call("RPCServer.CreateBreakpoint",
		createBreakpointIn{Breakpoint: breakpoint{File: sourceFile, Line: sourceLine}}, &bp))
	require.Equal(t, sourceLine, bp.Breakpoint.Line)

	// Send a request in the background. It blocks while the breakpoint is hit.
	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	respCh := make(chan *http.Response, 1)
	go func() {
		c := &http.Client{Timeout: 60 * time.Second}
		if resp, err := c.Get(url); err == nil { //nolint:noctx
			_ = resp.Body.Close()
			respCh <- resp
		}
		close(respCh)
	}()

	// Continue blocks until the target stops at the breakpoint.
	var state commandOut
	require.NoError(t, client.Call("RPCServer.Command", debuggerCommand{Name: "continue"}, &state))
	require.NotNil(t, state.State.CurrentThread, "the target did not stop")
	require.NotNil(t, state.State.CurrentThread.Breakpoint, "the target did not stop at a breakpoint")
	require.Equal(t, bp.Breakpoint.ID, state.State.CurrentThread.Breakpoint.ID)
	require.Equal(t, sourceFile, state.State.CurrentThread.File)
	require.Equal(t, sourceLine, state.State.CurrentThread.Line)
	require.Contains(t, state.State.CurrentThread.Function.Name, "example.(*Plugin).OnRequestHeaders")

	// Clear the breakpoint and resume Envoy without waiting: the request must then complete.
	var cleared clearBreakpointOut
	require.NoError(t, client.Call("RPCServer.ClearBreakpoint", clearBreakpointIn{ID: bp.Breakpoint.ID}, &cleared))
	client.Go("RPCServer.Command", debuggerCommand{Name: "continue"}, &commandOut{}, nil)

	select {
	case resp, ok := <-respCh:
		require.True(t, ok, "request failed")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "example-value", resp.Header.Get("x-example-response-header"))
	case <-time.After(60 * time.Second):
		t.Fatal("request did not complete after resuming the target")
	}

}

// Minimal types for the Delve JSON-RPC v2 API (github.com/go-delve/delve/service/rpc2).
type (
	breakpoint struct {
		ID   int    `json:"id"`
		File string `json:"file"`
		Line int    `json:"line"`
	}
	getVersionOut struct {
		DelveVersion string
	}
	createBreakpointIn struct {
		Breakpoint breakpoint
	}
	createBreakpointOut struct {
		Breakpoint breakpoint
	}
	clearBreakpointIn struct {
		ID int `json:"Id"`
	}
	clearBreakpointOut struct {
		Breakpoint *breakpoint
	}
	debuggerCommand struct {
		Name string `json:"name"`
	}
	commandOut struct {
		State struct {
			CurrentThread *struct {
				File       string      `json:"file"`
				Line       int         `json:"line"`
				Breakpoint *breakpoint `json:"breakPoint"`
				Function   *struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"currentThread"`
		}
	}
)
