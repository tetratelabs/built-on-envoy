// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestLuaRemoteExecution(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

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

func TestDockerRemoteExtension(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	// Run the remote extension in Docker.
	proc := internaltesting.RunCLI(t, cliBin, "run",
		"--docker",
		"--listen-port", "11000",
		"--dev",
		"--log-level", "dynamic_modules:debug",
		"--extension", "example-go")

	t.Cleanup(func() {
		_ = proc.Signal(syscall.SIGTERM)
		_, _ = proc.Wait()
	})

	require.Eventually(t, func() bool {
		return internaltesting.IsPortInUse(t.Context(), 11000)
	}, 2*time.Minute, 200*time.Millisecond, "Envoy did not start listening on port %d", 11000)

	url := "http://localhost:11000/status/200"
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-example-response-header") == "example-value"
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()

		assert.NoError(c, internaltesting.CheckGet(ctx, url, checkHeader))
	}, 2*time.Minute, 200*time.Millisecond)
}

func TestRustRemoteExtension(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	// Run the remote extension.
	// This will resolve the latest tag of the extension, download it to
	// the data directory, and execute it from there.
	proxyPort, _ := internaltesting.RunEnvoy(t, cliBin, "--log-level",
		"dynamic_modules:debug", "--extension", "ip-restriction",
		"--config", `{"deny_addresses": ["192.168.1.50"]}`)

	// Set X-Forwarded-For header to an IP address that should be denied by the ip-restriction extension.
	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkDenied := func(r *http.Response) bool {
		return r.StatusCode == http.StatusForbidden
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "192.168.1.50")

	require.NoError(t, internaltesting.CheckRequest(req, checkDenied))
}

func TestRustLocalExtension(t *testing.T) {
	t.Setenv("TEST_BOE_RUN_ENVOY_TIMEOUT", "5m")
	dataDir := t.TempDir()

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "rust-e2e",
		"--type", "rust",
		"--path", dataDir,
	)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Run the newly created extension
	proxyPort, _ := internaltesting.RunEnvoy(t, cliBin,
		"--log-level", "dynamic_modules:debug",
		"--local", dataDir+"/rust-e2e",
	)

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		headerValues, ok := r.Header[http.CanonicalHeaderKey("x-rust-e2e")]
		return ok && headerValues[0] == "example"
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}

func TestLocalGoExtension(t *testing.T) {
	// Configure the test env vars, as composer src will be downloaded from the registry
	internaltesting.SkipIfTestRegistryNotConfigured(t)
	dataDir := t.TempDir()

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "go-e2e", "--path", dataDir)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Add a dummy dependency to the extension to test that the extension can be built and run successfully
	// even the dependencies of the extension are not a subset of the composer's dependencies.
	addDummyDependencyToExtension(t, dataDir+"/go-e2e")

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

func addDummyDependencyToExtension(t *testing.T, path string) {
	// Create another module in the extension project as a dummy dependency for the extension.
	// This is to test that the extension can be built and run successfully even the dependencies
	// of the extension are not subset of the composer's dependencies.

	goModContent := `module inner
go 1.25.7
`

	goFileContent := `package inner
func Inner() string {
	return "inner"
}
`

	newModulePath := path + "/inner"

	err := os.Mkdir(newModulePath, 0o700)
	require.NoError(t, err, "failed to create inner module directory")

	goModPath := newModulePath + "/go.mod"
	err = os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod for inner module")

	goFilePath := newModulePath + "/inner.go"
	err = os.WriteFile(goFilePath, []byte(goFileContent), 0o600)
	require.NoError(t, err, "failed to write inner.go for inner module")

	// Append the content to go.mod.
	newDependencyContent := `require inner v0.0.0
replace inner => ./inner
`
	// #nosec G304
	f, err := os.OpenFile(path+"/go.mod", os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(newDependencyContent)
	require.NoError(t, err, "failed to write go.mod content")

	// Add some code in the parent project to use the go-e2e-inner extension.
	dummyGoFileContent := `
package main

import "inner"

func dummy() string {
	return inner.Inner()
}
`
	dummyGoFilePath := path + "/standalone/dummy.go"
	err = os.WriteFile(dummyGoFilePath, []byte(dummyGoFileContent), 0o600)
	require.NoError(t, err, "failed to write dummy go file")

	// Run `go mod tidy` to make sure the dependencies are properly resolved.
	goModTidyCmd := exec.Command("go", "mod", "tidy")
	goModTidyCmd.Dir = path
	output, err := goModTidyCmd.CombinedOutput()
	require.NoError(t, err, string(output))
}
