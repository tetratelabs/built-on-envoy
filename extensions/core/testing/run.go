// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package internaltesting provides utilities for testing BOE extensions,
// such as running a BOE process and waiting for it to be ready.
package internaltesting

import (
	"bytes"
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
)

var (
	// boeRunTimeout is the timeout for BOERun, which can be set via
	// the BOE_TEST_RUN_TIMEOUT environment variable.
	boeRunTimeout = cmp.Or(os.Getenv("BOE_TEST_RUN_TIMEOUT"), "30s")
	// boeLogLevel is the log level for the Envoy process,
	// which can be set via the BOE_TEST_LOG_LEVEL environment variable.
	boeLogLevel = cmp.Or(os.Getenv("BOE_TEST_LOG_LEVEL"), "all:warning")
)

// BOERun runs the BOE binary with the given arguments and returns the port it's listening on.
// It also sets up a cleanup function to gracefully shut down the process after the test finishes.
func BOERun(t *testing.T, args ...string) int {
	boeBin := cmp.Or(os.Getenv("BOE_BIN"), "boe")
	t.Logf("Using BOE binary: %s", boeBin)

	waitTimeout, err := time.ParseDuration(boeRunTimeout)
	require.NoError(t, err)

	proxyPort, err := getFreePort()
	require.NoError(t, err)
	adminPort, err := getFreePort()
	require.NoError(t, err)

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

	args = append([]string{
		"run",
		"--listen-port", strconv.Itoa(proxyPort),
		"--admin-port", strconv.Itoa(adminPort),
		"--log-level", boeLogLevel,
	}, args...)

	t.Logf("Starting Envoy %v...", args)

	cmd := exec.CommandContext(cmdCtx, boeBin, args...) // #nosec G204
	cmd.WaitDelay = 3 * time.Second                     // auto-kill after 3 seconds.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Start())
	process := cmd.Process

	t.Cleanup(func() {
		defer cancel()
		// Signal graceful shutdown
		if err := process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to send interrupt to boe process: %v", err)
		}
		// Wait for the process to exit gracefully, in worst case this is
		// killed in 3 seconds by WaitDelay configured in the command.
		// In that case, you may have a zombie Envoy process left behind!
		if _, err := process.Wait(); err != nil {
			t.Logf("Failed to wait for boe process to exit: %v", err)
		}

		if t.Failed() {
			t.Logf("=== boe Stdout ===\n%s", stdout.String())
			t.Logf("=== boe Stderr ===\n%s", stderr.String())
		}
	})

	// Wait until the process is listening on the port before returning,
	// otherwise tests may try to send requests before Envoy is ready, causing flakes.
	require.Eventually(t, func() bool {
		return isPortInUse(t.Context(), proxyPort)
	}, waitTimeout, 250*time.Millisecond, "boe did not start listening on port %d", proxyPort)

	t.Logf("boe process started with PID %d", process.Pid)

	return proxyPort
}

// getFreePort finds an available port.
func getFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// isPortInUse checks if a port is in use (returns true if listening).
func isPortInUse(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}
