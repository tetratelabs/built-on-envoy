// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This direct func-e runner lets E2E tests exercise bootstrap-only Envoy
// features, such as typed_per_filter_config, without expanding the BOE CLI.
package internaltesting

import (
	"context"
	"testing"

	funce "github.com/tetratelabs/func-e"
	"github.com/tetratelabs/func-e/api"
)

// RunEnvoyYAML starts Envoy from a complete bootstrap YAML document. Environment
// variables are applied only for the lifetime of the test and Envoy is stopped
// during cleanup.
func RunEnvoyYAML(t *testing.T, listenPort, adminPort int, config string, env map[string]string, envoyArgs ...string) {
	t.Helper()
	t.Logf("Starting Envoy from bootstrap YAML on listener port %d", listenPort)

	for key, value := range env {
		t.Setenv(key, value)
	}

	logDir := t.TempDir()
	buffers := DumpLogsOnFail(t, logDir, "Envoy stdout", "Envoy stderr")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		args := append([]string{
			"--config-yaml", config,
			"--log-level", "error",
			"--use-dynamic-base-id",
		}, envoyArgs...)
		done <- funce.Run(ctx, args, api.StateHome(logDir), api.RuntimeDir(logDir), api.Out(buffers[0]), api.EnvoyOut(buffers[0]), api.EnvoyErr(buffers[1]))
	}()

	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && t.Failed() {
			t.Logf("Envoy exited with error: %v", err)
		}
	})
	AwaitAdminReady(t, adminPort)
}
