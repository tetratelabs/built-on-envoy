// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestDefaultProxy(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200)))
	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/server_info", adminPort), internaltesting.EqualStatus(200)))
}

func TestCustomPorts(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200)))
	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/server_info", adminPort), internaltesting.EqualStatus(200)))
}

func TestLuaRemoteExecution(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	// Run the remote extension.
	// This will resolve the latest tag of the extension, download it to
	// the data directory, and execute it from there.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], "--log-level", "lua:info", "--extension", "example-lua")

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-lua-response-processed") == "true"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}

func TestDevEnvoyVersion(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort, "--envoy-version", "dev-latest")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200)))
}

func TestLuaLocalExtension(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
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

	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]

	// Run the remote extension in Docker.
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--docker",
		"--dev",
		"--log-level", "dynamic_modules:debug",
		"--extension", "example-go:0.3.0")

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
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
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], "--log-level",
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
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
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

func TestExtProcLocalExtension(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], "--log-level",
		"ext_proc:debug", "--local", "../../extensions/example-ext-proc")

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-ext-proc") == "processed"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}

func TestExtProcRemoteExtension(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	// Run the remote extension.
	// This will resolve the latest tag of the extension, download it to
	// the data directory, and execute it from there.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], "--log-level",
		"ext_proc:debug", "--extension", "example-ext-proc")

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-ext-proc") == "processed"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}

func TestLocalGoExtension(t *testing.T) {
	testLocalGoExtension(t, false)
}

func TestLocalGoExtensionLegacyPluginPath(t *testing.T) {
	testLocalGoExtension(t, true)
}

func testLocalGoExtension(t *testing.T, removeCSharedMain bool) {
	t.Helper()

	// Local builds for Go will pull libcomposer from the remote registry. However, when we're doing changes to versions, etc, we don't want it to
	// pull an obsolete version, so we'll just push the current composer source to the local registry and use that for the test.
	t.Setenv("BOE_REGISTRY", registryAddr)
	t.Setenv("BOE_REGISTRY_INSECURE", "true")
	makeCmd := exec.CommandContext(t.Context(), "make", "push_code")
	makeCmd.Dir = "../../extensions/composer"
	output, err := makeCmd.CombinedOutput()
	t.Logf("make push_code output: %s", string(output))
	require.NoError(t, err)

	dataDir := t.TempDir()

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "go-e2e", "--path", dataDir)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	if removeCSharedMain {
		require.NoError(t, os.RemoveAll(dataDir+"/go-e2e/main"))
	}

	// Add a dummy dependency to the extension to test that the extension can be built and run successfully
	// even the dependencies of the extension are not a subset of the composer's dependencies.
	addDummyDependencyToExtension(t, dataDir+"/go-e2e")

	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
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
go 1.26.3
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

// TestNativeHTTPFilterPositionExtensions verifies that nativeHttpFilters.before
// and .after control filter ordering relative to the extension's own filter.
func TestNativeHTTPFilterPositionExtensions(t *testing.T) {
	tests := []struct {
		name                    string
		local                   string
		expectedResponseHeaders http.Header
	}{
		{
			name:  "before: native filter classifies tenant for lua enrichment",
			local: "testdata/lua_with_header_to_metadata_before",
			expectedResponseHeaders: http.Header{
				"X-Upstream-Tenant-Id":     {""},         // stripped before lua saw it
				"X-Upstream-Tenant-Tier":   {"premium"},  // lua enriched from metadata
				"X-Upstream-Tenant-Source": {"metadata"}, // proves lua read metadata, not header
				"X-Tenant-From-Metadata":   {"acme"},
			},
		},
		{
			name:  "after: lua normalizes tenant for native filter metadata",
			local: "testdata/lua_with_header_to_metadata_after",
			expectedResponseHeaders: http.Header{
				"X-Upstream-Tenant-Id":        {""},        // lua stripped original
				"X-Upstream-Tenant-Tier":      {"premium"}, // lua enriched from header
				"X-Upstream-Tenant-Source":    {"header"},  // proves lua read header, not metadata
				"X-Upstream-Canonical-Tenant": {""},        // carrier stripped by after filter
				"X-Tenant-From-Metadata":      {"acme"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-upstream-tenant-id", r.Header.Get("x-tenant-id"))
				w.Header().Set("x-upstream-tenant-tier", r.Header.Get("x-tenant-tier"))
				w.Header().Set("x-upstream-tenant-source", r.Header.Get("x-tenant-source"))
				w.Header().Set("x-upstream-canonical-tenant", r.Header.Get("x-canonical-tenant"))
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(upstream.Close)

			upstreamAddr := upstream.Listener.Addr().String()
			ports := internaltesting.FreePorts(t, 2)
			proxyPort := ports[0]
			internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
				"--log-level", "lua:info",
				"--cluster-insecure", upstreamAddr,
				"--test-upstream-cluster", upstreamAddr,
				"--local", tt.local,
			)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(cancel)

			req, err := http.NewRequestWithContext(ctx,
				http.MethodGet,
				fmt.Sprintf("http://localhost:%d/anything", proxyPort),
				nil)
			require.NoError(t, err)
			req.Header = http.Header{"X-Tenant-Id": {"acme"}}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close() // nolint:errcheck

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Subset(t, resp.Header, tt.expectedResponseHeaders)
		})
	}
}
