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
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/func-e/experimental/admin"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestDefaultProxy(t *testing.T) {
	adminPort := runEnvoy(t, cliBin, nil, "run")
	require.NoError(t, checkEndpoint(t.Context(), fmt.Sprintf("http://localhost:%d/status/200", proxyPort), statusEq(200)))
	require.NoError(t, checkEndpoint(t.Context(), fmt.Sprintf("http://localhost:%d/server_info", adminPort), statusEq(200)))
}

func TestCustomPorts(t *testing.T) {
	_ = runEnvoy(t, cliBin, nil, "run", "--listen-port", "11000", "--admin-port", "12000")
	require.NoError(t, checkEndpoint(t.Context(), "http://localhost:11000/status/200", statusEq(200)))
	require.NoError(t, checkEndpoint(t.Context(), "http://localhost:12000/server_info", statusEq(200)))
}

// runEnvoy starts the CLI as a subprocess with the given config file.
func runEnvoy(t *testing.T, cliBin string, env []string, arg ...string) (adminPort int) {
	// Wait up to 10 seconds for both ports to be frboe.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for isPortInUse(ctx, proxyPort) {
		select {
		case <-ctx.Done():
			require.FailNow(t, "Ports still in use after timeout",
				"Port %d is still in use", proxyPort)
		case <-time.After(500 * time.Millisecond):
			// Retry after a short delay.
		}
	}

	// Capture logs, only dump on failure.
	buffers := internaltesting.DumpLogsOnFail(t, "boe Stdout", "boe Stderr")

	t.Logf("Starting boe with args: %v", arg)
	// Note: do not pass t.Context() to CommandContext, as it's canceled
	// *before* t.Cleanup functions are called.
	//
	// > Context returns a context that is canceled just before
	// > Cleanup-registered functions are called.
	//
	// That means the subprocess gets killed before we can send it an interrupt
	// signal for graceful shutdown, which results in orphaned subprocesses.
	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, cliBin, arg...)
	cmd.Stdout = buffers[0]
	cmd.Stderr = buffers[1]
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = 3 * time.Second // auto-kill after 3 seconds.

	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		defer cancel()
		// Don't use require.XXX inside cleanup functions as they call
		// runtime.Goexit preventing further cleanup functions from running.

		// Graceful shutdown, should kill the Envoy subprocess, too.
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to send interrupt to boe process: %v", err)
		}
		// Wait for the process to exit gracefully, in worst case this is
		// killed in 3 seconds by WaitDelay above. In that case, you may
		// have a zombie Envoy process left behind!
		if _, err := cmd.Process.Wait(); err != nil {
			t.Logf("Failed to wait for boe process to exit: %v", err)
		}

		// Delete the hard-coded path to certs defined in Envoy AI Gateway
		if err := os.RemoveAll("/tmp/envoy-gateway/certs"); err != nil {
			t.Logf("Failed to delete envoy gateway certs: %v", err)
		}
	})

	t.Logf("boe process started with PID %d", cmd.Process.Pid)

	t.Log("Waiting for boe to start (Envoy admin endpoint)...")

	adminClient, err := admin.NewAdminClient(t.Context(), cmd.Process.Pid)
	require.NoError(t, err)

	err = adminClient.AwaitReady(t.Context(), time.Second)
	require.NoError(t, err)

	t.Log("boe CLI is ready")
	return adminClient.Port()
}

// Function to check if a port is in use (returns true if listening).
func isPortInUse(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

// statusIs returns a condition function that checks if the response status code matches the given code.
func statusEq(code int) func(r *http.Response) bool {
	return func(r *http.Response) bool {
		return r.StatusCode == code
	}
}

// checkEndpoint checks if the given HTTP endpoint is available
// according to the provided condition.
func checkEndpoint(ctx context.Context, url string, condition func(r *http.Response) bool) error {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint:errcheck
	if !condition(resp) {
		return fmt.Errorf("condition not met (status: %d)", resp.StatusCode)
	}
	return nil
}
