// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestDefaultProxy(t *testing.T) {
	proxyPort, adminPort := internaltesting.RunEnvoy(t, cliBin)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200)))
	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/server_info", adminPort), internaltesting.EqualStatus(200)))
}

func TestCustomPorts(t *testing.T) {
	_, _ = internaltesting.RunEnvoy(t, cliBin, "--listen-port", "11000", "--admin-port", "12000")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, "http://localhost:11000/status/200", internaltesting.EqualStatus(200)))
	require.NoError(t, internaltesting.CheckGet(ctx, "http://localhost:12000/server_info", internaltesting.EqualStatus(200)))
}

func TestLuaLocalExtension(t *testing.T) {
	proxyPort, _ := internaltesting.RunEnvoy(t, cliBin,
		"--log-level", "lua:info",
		"--local", "../../extensions/example-lua",
	)

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-lua-response-processed") == "true"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}

func TestExtensionPull(t *testing.T) {
	t.Setenv("BOE_REGISTRY", registryAddr)
	t.Setenv("BOE_REGISTRY_INSECURE", "true")

	// Push the extension to the test registry
	process := internaltesting.RunCLI(t, cliBin, "push", "../../extensions/example-lua")
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Pull the extension to a local directory
	tmpDir := t.TempDir()
	process = internaltesting.RunCLI(t, cliBin, "pull", "src-example-lua", "--path", tmpDir)
	status, err = process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Vefrify the extension has been downloaded
	manifestFile := fmt.Sprintf("%s/extensions/src-example-lua/1.0.0/manifest.yaml", tmpDir)
	maniefst, err := extensions.LoadLocalManifest(manifestFile)
	require.NoError(t, err)
	require.Equal(t, "example-lua", maniefst.Name)
	require.Equal(t, "1.0.0", maniefst.Version)
}

func TestLuaRemoteExecution(t *testing.T) {
	t.Setenv("BOE_REGISTRY", registryAddr)
	t.Setenv("BOE_REGISTRY_INSECURE", "true")

	// Push the extension to the test registry
	process := internaltesting.RunCLI(t, cliBin, "push", "../../extensions/example-lua")
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Run the remote extension.
	// This will resolve the latest tag of the extension, download it to
	// the data directory, and execute it from there.
	proxyPort, _ := internaltesting.RunEnvoy(t, cliBin, "--log-level", "lua:info", "--extension", "example-lua")

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-lua-response-processed") == "true"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}

func TestLocalGoExtension(t *testing.T) {
	dataDir := t.TempDir()

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "go-e2e", "--path", dataDir)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	proxyPort, _ := internaltesting.RunEnvoy(t, cliBin,
		"--local", dataDir+"/go-e2e",
		"--local", dataDir+"/go-e2e",
		"--config", "{}",
		"--config", `{"header_value":"configured-value"}`, // test config for second local extension
		"--log-level", "dynamic_modules:debug",
	)

	// For the response, the execution order of the extensions is in reverse order of the
	// declaration order, so the header from the second extension should come first.
	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		headerValues, ok := r.Header[http.CanonicalHeaderKey("x-go-e2e")]
		return ok && len(headerValues) == 2 &&
			headerValues[0] == "configured-value" &&
			headerValues[1] == "example"
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}
