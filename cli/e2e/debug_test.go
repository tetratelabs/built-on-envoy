// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"context"
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
	// boe builds its own Delve. Go extensions are debugged in a container on non-Linux platforms.
	if runtime.GOOS != "linux" {
		if err := exec.Command("docker", "info").Run(); err != nil {
			t.Skipf("docker is required to debug Go extensions on %s", runtime.GOOS)
		}
	}

	extensionDir, err := filepath.Abs("../../extensions/composer/example")
	require.NoError(t, err)
	sourceFile := filepath.Join(extensionDir, "example.go")
	const sourceLine = 36 // First line in the body of example.(*Plugin).OnRequestHeaders

	ports := internaltesting.FreePorts(t, 3)
	proxyPort, adminPort, dlvPort := ports[0], ports[1], ports[2]
	internaltesting.RunBoe(t, cliBin, "debug", proxyPort, adminPort,
		"--debug-port", strconv.Itoa(dlvPort),
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

	// Delve is started with --continue, so the target is running and Delve does not process requests that
	// change the target state until it stops. Halt it first, as IDEs do before setting breakpoints.
	var halted commandOut
	callDelve(t, client, "RPCServer.Command", debuggerCommand{Name: "halt"}, &halted)

	// Set a breakpoint by file and line, as an IDE does. The source paths in the debug information must
	// match the host paths, even when Envoy runs in a container.
	var bp createBreakpointOut
	callDelve(t, client, "RPCServer.CreateBreakpoint",
		createBreakpointIn{Breakpoint: breakpoint{File: sourceFile, Line: sourceLine}}, &bp)
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
	callDelve(t, client, "RPCServer.Command", debuggerCommand{Name: "continue"}, &state)
	require.NotNil(t, state.State.CurrentThread, "the target did not stop")
	require.NotNil(t, state.State.CurrentThread.Breakpoint, "the target did not stop at a breakpoint")
	require.Equal(t, bp.Breakpoint.ID, state.State.CurrentThread.Breakpoint.ID)
	require.Equal(t, sourceFile, state.State.CurrentThread.File)
	require.Equal(t, sourceLine, state.State.CurrentThread.Line)
	require.Contains(t, state.State.CurrentThread.Function.Name, "example.(*Plugin).OnRequestHeaders")

	// Clear the breakpoint and resume Envoy without waiting: the request must then complete.
	var cleared clearBreakpointOut
	callDelve(t, client, "RPCServer.ClearBreakpoint", clearBreakpointIn{ID: bp.Breakpoint.ID}, &cleared)
	client.Go("RPCServer.Command", debuggerCommand{Name: "continue"}, &commandOut{}, nil)

	select {
	case resp, ok := <-respCh:
		require.True(t, ok, "request failed")
		// Do not check the status code: when Envoy runs in a container the test upstream in the host
		// loopback is not reachable. The response header proves the request went through the extension.
		require.Equal(t, "example-value", resp.Header.Get("x-example-response-header"))
	case <-time.After(60 * time.Second):
		t.Fatal("request did not complete after resuming the target")
	}
}

// TestDebugLocalRust runs a Rust extension with `boe debug`, connects LLDB to the GDB remote debug server attached
// to Envoy (as an IDE would do), sets a breakpoint by file and line in the extension source, and verifies the
// breakpoint is hit when a request is sent to Envoy. It connects twice to verify debuggers can reconnect, and
// that Envoy keeps serving requests when no debugger is connected.
func TestDebugLocalRust(t *testing.T) {
	internaltesting.MaybeSkipLongRunningTest(t)
	lldb, err := exec.LookPath("lldb")
	if err != nil {
		t.Skip("lldb is required to connect to the debug server")
	}
	if _, err = exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required to build Rust extensions")
	}
	if runtime.GOOS == "linux" {
		if _, err = exec.LookPath("gdbserver"); err != nil {
			t.Skip("gdbserver is required to debug Rust extensions on Linux")
		}
	}

	extensionDir, err := filepath.Abs("../../extensions/ip-restriction")
	require.NoError(t, err)
	sourceFile := filepath.Join(extensionDir, "src", "lib.rs")
	const sourceLine = 144 // First statement in Filter::on_request_headers

	ports := internaltesting.FreePorts(t, 3)
	proxyPort, adminPort, debugPort := ports[0], ports[1], ports[2]
	internaltesting.RunBoe(t, cliBin, "debug", proxyPort, adminPort,
		"--debug-port", strconv.Itoa(debugPort),
		// The minimum Envoy version of ip-restriction does not support the generated dynamic module config.
		"--envoy-version", "1.38.0",
		"--local", extensionDir,
		"--config", `{"deny_addresses":["192.168.1.50"]}`,
	)
	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/ready", adminPort), internaltesting.EqualStatus(http.StatusOK))

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	for session := range 2 {
		// The debug server is only attached while a debugger is connected, so requests go through.
		internaltesting.RequireEventuallyGet(t, url, internaltesting.EqualStatus(http.StatusOK))

		// Send a request once the breakpoint is set. It blocks while the breakpoint is hit.
		respCh := make(chan int, 1)
		go func() {
			time.Sleep(10 * time.Second)
			c := &http.Client{Timeout: 60 * time.Second}
			resp, err := c.Get(url) //nolint:noctx
			if err != nil {
				respCh <- 0
				return
			}
			_ = resp.Body.Close()
			respCh <- resp.StatusCode
		}()

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		out, err := exec.CommandContext(ctx, lldb, "--batch", //nolint:gosec // test command
			"-o", "gdb-remote 127.0.0.1:"+strconv.Itoa(debugPort),
			"-o", fmt.Sprintf("breakpoint set -f %s -l %d", sourceFile, sourceLine),
			"-o", "continue",
			"-o", "frame variable _end_stream",
			"-o", "detach",
		).CombinedOutput()
		cancel()
		require.NoError(t, err, "session %d: %s", session, out)
		require.Contains(t, string(out), "stop reason = breakpoint 1.1", "session %d: %s", session, out)
		require.Contains(t, string(out), "on_request_headers", "session %d: %s", session, out)
		require.Contains(t, string(out), "_end_stream = true", "session %d: %s", session, out)

		// After detaching, Envoy keeps running and the request completes.
		select {
		case status := <-respCh:
			require.Equal(t, http.StatusOK, status, "session %d: request failed", session)
		case <-time.After(60 * time.Second):
			t.Fatalf("session %d: request did not complete after detaching", session)
		}
	}
}

// delveCallTimeout is how long to wait for a Delve API call to complete.
const delveCallTimeout = 60 * time.Second

// callDelve calls the given Delve API method and fails the test if it errors or does not complete in time,
// instead of blocking forever.
func callDelve(t *testing.T, client *rpc.Client, method string, args, reply any) {
	t.Helper()
	select {
	case call := <-client.Go(method, args, reply, nil).Done:
		require.NoError(t, call.Error, "Delve call %s failed", method)
	case <-time.After(delveCallTimeout):
		t.Fatalf("Delve call %s did not complete in %v", method, delveCallTimeout)
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
