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

var (
	// defaultListenPort is the default port where the proxy listens.
	defaultListenPort = 10000
	// defaultLogLevel is the default log level for the proxy.
	defaultLogLevel = "all:debug"
	// defaultRunEnvoyTimeout is the default timeout for waiting for Envoy to start.
	defaultRunEnvoyTimeout = 90 * time.Second
)

// runEnvoyTimeout returns the timeout duration for waiting for Envoy to start.
func runEnvoyTimeout() time.Duration {
	timeout, _ := time.ParseDuration(os.Getenv("TEST_BOE_RUN_ENVOY_TIMEOUT"))
	return cmp.Or(timeout, defaultRunEnvoyTimeout)
}

// RunEnvoy executes the "run" command of the CLI binary to start Envoy with given args.
// It waits until Envoy is ready to serve traffic and returns the listen port and admin port.
func RunEnvoy(t *testing.T, cliBin string, args ...string) (listenPort int, adminPort int) {
	proxyPort := defaultListenPort
	hasLogLevel := false
	for i, arg := range args {
		if arg == "--listen-port" && i+1 < len(arg) {
			var err error
			proxyPort, err = strconv.Atoi(args[i+1])
			require.NoError(t, err)
		}
		if arg == "--log-level" {
			hasLogLevel = true
		}
	}

	// Wait up to 10 seconds for both ports to be free.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for IsPortInUse(ctx, proxyPort) {
		select {
		case <-ctx.Done():
			require.FailNow(t, "Ports still in use after timeout",
				"Port %d is still in use", proxyPort)
		case <-time.After(500 * time.Millisecond):
			// Retry after a short delay.
		}
	}

	// Set the default log level if not set.
	if !hasLogLevel {
		args = append(args, "--log-level", defaultLogLevel)
	}
	args = append([]string{"run"}, args...)

	// Run the command
	process := RunCLI(t, cliBin, args...)

	t.Cleanup(func() {
		defer cancel()
		// Don't use require.XXX inside cleanup functions as they call
		// runtime.Goexit preventing further cleanup functions from running.

		// Graceful shutdown, should kill the Envoy subprocess, too.
		if err := process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to send interrupt to boe process: %v", err)
		}
		// Wait for the process to exit gracefully, in worst case this is
		// killed in 3 seconds by WaitDelay conigured int he command.
		// In that case, you may have a zombie Envoy process left behind!
		if _, err := process.Wait(); err != nil {
			t.Logf("Failed to wait for boe process to exit: %v", err)
		}
	})

	t.Logf("Waiting for boe to start (Envoy listening on %d)...", proxyPort)

	// Wait fist for the main Envoy listener.
	// The func-e admin client relies on Envoy being the first child process ot be able to retrieve
	// the admin port.
	// This may not be the case when running local go extensions that execute commands to compile the plugin
	// on the first run, so we wait first for Envoy to be listening on the proxy port, then cehck the admin server
	// as we know there won't be other interfering child processes at that point.
	require.Eventually(t, func() bool {
		return IsPortInUse(t.Context(), proxyPort)
	}, runEnvoyTimeout(), 100*time.Millisecond, "Envoy did not start listening on port %d", proxyPort)

	adminClient, err := admin.NewAdminClient(t.Context(), process.Pid)
	require.NoError(t, err)

	err = adminClient.AwaitReady(t.Context(), time.Second)
	require.NoError(t, err)

	t.Log("boe CLI is ready")
	return proxyPort, adminClient.Port()
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
