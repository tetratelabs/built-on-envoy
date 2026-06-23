// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testUpstreamArgs returns CLI arguments for the test upstream cluster if set via environment variables.
func testUpstreamArgs() []string {
	if testUpstream := TestUpstreamCluster.Get(); testUpstream != "" {
		return []string{"--cluster", testUpstream, "--test-upstream-cluster", testUpstream}
	}
	if testUpstream := TestUpstreamClusterInsecure.Get(); testUpstream != "" {
		return []string{"--cluster-insecure", testUpstream, "--test-upstream-cluster", testUpstream}
	}
	return nil
}

// RunEnvoy executes the CLI run command on the given listener and admin ports.
func RunEnvoy(t *testing.T, cliBin string, listenPort int, adminPort int, args ...string) {
	args = append([]string{
		"run",
		"--listen-port", strconv.Itoa(listenPort),
		"--admin-port", strconv.Itoa(adminPort),
	}, append(testUpstreamArgs(), args...)...)

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

	require.Eventually(t, func() bool {
		return IsPortInUse(t.Context(), listenPort)
	}, RunEnvoyTimeout.Get(), 100*time.Millisecond, "Envoy did not start listening on port %d", listenPort)

	AwaitAdminReady(t, adminPort)

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
	logDir := t.TempDir()

	var buffers OutBuffers
	if teeFile := TestCLIOutputFile.Get(); teeFile != "" {
		buffers = TeeOutput(t, teeFile, "boe Stdout", "boe Stderr")
	} else {
		buffers = DumpLogsOnFail(t, logDir, "boe Stdout", "boe Stderr")
	}

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
	cmd.Env = append(os.Environ(), fmt.Sprintf("BOE_STATE_HOME=%s", logDir))

	require.NoError(t, cmd.Start())

	t.Logf("boe process started with PID %d", cmd.Process.Pid)

	return cmd.Process
}

// AwaitAdminReady polls the Envoy admin /ready endpoint until it returns
// "LIVE" or the test's context is canceled.
func AwaitAdminReady(t *testing.T, adminPort int) {
	t.Helper()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/ready", adminPort)
	client := &http.Client{Timeout: time.Second}

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, http.NoBody)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close() //nolint:errcheck
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		return resp.StatusCode == http.StatusOK && strings.EqualFold(strings.TrimSpace(string(body)), "live")
	}, RunEnvoyTimeout.Get(), 100*time.Millisecond, "Envoy admin not ready on port %d", adminPort)
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
