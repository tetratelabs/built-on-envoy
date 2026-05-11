// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/func-e/experimental/admin"
)

// defaultRunEnvoyTimeout is the default timeout for waiting for Envoy to start.
const defaultRunEnvoyTimeout = 90 * time.Second

// runEnvoyTimeout returns the timeout duration for waiting for Envoy to start.
func runEnvoyTimeout() time.Duration {
	timeout, _ := time.ParseDuration(os.Getenv("TEST_BOE_RUN_ENVOY_TIMEOUT"))
	return cmp.Or(timeout, defaultRunEnvoyTimeout)
}

// RunEnvoy executes the CLI run command on the given listener and admin ports.
func RunEnvoy(t *testing.T, cliBin string, listenPort int, adminPort int, args ...string) {
	args = append([]string{
		"run",
		"--listen-port", strconv.Itoa(listenPort),
		"--admin-port", strconv.Itoa(adminPort),
	}, args...)

	process := RunCLI(t, cliBin, args...)

	t.Cleanup(func() {
		// Don't use require.XXX inside cleanup functions as they call
		// runtime.Goexit preventing further cleanup functions from running.

		// Graceful shutdown, should kill the Envoy subprocess, too.
		if err := process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to send interrupt to boe process: %v", err)
		}
		// Wait for the process to exit gracefully, in worst case this is
		// killed in 3 seconds by WaitDelay configured in the command.
		// In that case, you may have a zombie Envoy process left behind!
		if _, err := process.Wait(); err != nil {
			t.Logf("Failed to wait for boe process to exit: %v", err)
		}
	})

	t.Logf("Waiting for boe to start (Envoy listening on %d)...", listenPort)

	// Wait for Envoy to bind its listener before using func-e's admin
	// discovery. Local extensions can compile helper processes before Envoy
	// starts, and admin discovery expects Envoy to be the relevant child.
	require.Eventually(t, func() bool {
		return IsPortInUse(t.Context(), listenPort)
	}, runEnvoyTimeout(), 100*time.Millisecond, "Envoy did not start listening on port %d", listenPort)

	adminClient, err := admin.NewAdminClient(t.Context(), process.Pid)
	require.NoError(t, err)

	err = adminClient.AwaitReady(t.Context(), time.Second)
	require.NoError(t, err)
	require.Equal(t, adminPort, adminClient.Port())

	t.Log("boe CLI is ready")
}

// FreePorts returns available loopback TCP ports for tests that must pass ports
// explicitly to a process.
func FreePorts(t *testing.T, count int) []int {
	t.Helper()

	listeners := make([]net.Listener, 0, count)
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()

	ports := make([]int, 0, count)
	for i := 0; i < count; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

// RunCLI starts the CLI as a subprocess with the given arguments.
func RunCLI(t *testing.T, cliBin string, args ...string) *os.Process {
	// Capture logs, only dump on failure.
	buffers := DumpLogsOnFail(t, "boe Stdout", "boe Stderr")

	t.Logf("Starting boe with args: %v", args)

	// Note: do not pass t.Context() to CommandContext, as it's canceled
	// *before* t.Cleanup functions are called.
	//
	// > Context returns a context that is canceled just before
	// > Cleanup-registered functions are called.
	//
	// That means the subprocess gets killed before we can send it an interrupt
	// signal for graceful shutdown, which results in orphaned subprocesses.
	cmdCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(cmdCtx, cliBin, args...)
	cmd.Stdout = buffers[0]
	cmd.Stderr = buffers[1]
	cmd.WaitDelay = 3 * time.Second // auto-kill after 3 seconds.

	require.NoError(t, cmd.Start())

	t.Logf("boe process started with PID %d", cmd.Process.Pid)

	return cmd.Process
}

// IsPortInUse checks if a port is in use (returns true if listening).
func IsPortInUse(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}
