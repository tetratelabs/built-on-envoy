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
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestDefaultProxy(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort)

	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200))
	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/server_info", adminPort), internaltesting.EqualStatus(200))
}

func TestCustomPorts(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort)

	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200))
	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/server_info", adminPort), internaltesting.EqualStatus(200))
}

func TestLuaRemoteExecution(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	// Run the remote extension.
	// This will resolve the latest tag of the extension, download it to
	// the data directory, and execute it from there.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], "--log-level", "lua:info", "--extension", "example-lua")

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-lua-response-processed") == "true"
		})
}

func TestDevEnvoyVersion(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort, "--envoy-version", "dev-latest")

	internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200))
}

func TestLuaLocalExtension(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--log-level", "lua:info",
		"--local", "../../extensions/example-lua",
	)

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-lua-response-processed") == "true"
		})
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

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-example-response-header") == "example-value"
		})
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

	ctx, cancel := context.WithTimeout(t.Context(), internaltesting.TestRequestTimeout.Get())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "192.168.1.50")

	internaltesting.RequireEventuallyRequest(t, req, func(r *http.Response) bool {
		return r.StatusCode == http.StatusForbidden
	})
}

func TestRustLocalExtension(t *testing.T) {
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	dataDir := t.TempDir()

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "rust-e2e",
		"--type", "rust",
		"--path", dataDir,
	)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Overwrite the dependency in Cargo.toml to the stable version of Envoy 1.38 to
	// avoid potential compatibility issues make the CI flaky.
	// TODO(wbpcode): remove this if we get the dependency more stable.
	cargoTomlPath := dataDir + "/rust-e2e/Cargo.toml"
	// #nosec G304
	cargoToml, err := os.ReadFile(cargoTomlPath)
	require.NoError(t, err)
	cargoToml = regexp.MustCompile(`rev = "[0-9a-f]+"`).
		ReplaceAll(cargoToml, []byte(`rev = "f1dd21b16c244bda00edfb5ffce577e12d0d2ec2"`))
	require.NoError(t, os.WriteFile(cargoTomlPath, cargoToml, 0o600))

	// Run the newly created extension
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--log-level", "dynamic_modules:debug",
		"--envoy-version", "dev-latest",
		"--local", dataDir+"/rust-e2e",
	)

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			headerValues, ok := r.Header[http.CanonicalHeaderKey("x-rust-e2e")]
			return ok && headerValues[0] == "example"
		})
}

func TestExtProcLocalExtension(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], "--log-level",
		"ext_proc:debug", "--local", "../../extensions/example-ext-proc")

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-ext-proc") == "processed"
		})
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

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-ext-proc") == "processed"
		})
}

// TestWasmLocalExtension scaffolds a Wasm extension with `boe create --type wasm`, then runs it
// with `--local`. This exercises the local build path (BuildWasm compiles the Go source to a
// wasip1/wasm c-shared module) and the plugin configuration path (the --config string is delivered
// to the module and surfaced via proxy_on_configure).
func TestWasmLocalExtension(t *testing.T) {
	// Creating the extension (go mod tidy), compiling the Wasm module, and starting Envoy can take
	// a while.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	dataDir := t.TempDir()

	// Create a brand new Wasm extension.
	process := internaltesting.RunCLI(t, cliBin, "create", "wasm-e2e",
		"--type", "wasm",
		"--path", dataDir,
	)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Run the newly created extension, passing a custom header value to exercise config delivery.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--log-level", "wasm:info",
		"--local", dataDir+"/wasm-e2e",
		"--config", `{"header_value":"local-value"}`,
	)

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-wasm-header") == "local-value"
		})
}

// TestWasmRemoteExtension scaffolds a Wasm extension with `boe create --type wasm`, pushes it to
// the local test registry via the scaffolded Makefile's push_image target (an architecture-
// independent single manifest), then runs it remotely with `--extension`. This exercises the full
// remote flow: pull the compiled module from the registry into the local cache and run it.
func TestWasmRemoteExtension(t *testing.T) {
	// Creating, building the image, pushing, and starting Envoy can take a while.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	// Point boe and the build tooling at the local insecure registry. Setting BOE_REGISTRY makes
	// the scaffolded Makefile's HUB resolve to the registry address, so push_image publishes to
	// <registry>/extension-wasm-e2e:<version>, which is exactly what `--extension wasm-e2e` resolves to.
	testRegistry.Configure(t)

	// Dedicated data home so the pull cache is isolated from the host.
	t.Setenv("BOE_DATA_HOME", t.TempDir())

	dataDir := t.TempDir()

	// Create a brand new Wasm extension.
	process := internaltesting.RunCLI(t, cliBin, "create", "wasm-e2e",
		"--type", "wasm",
		"--path", dataDir,
	)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	// Build and push the architecture-independent image to the local registry via the scaffolded
	// Makefile. The extension version comes from the scaffolded manifest (0.1.0).
	runCmd(t, dataDir+"/wasm-e2e", "make", "push_image")

	// Run the remote extension: this resolves wasm-e2e:0.1.0 against BOE_REGISTRY, downloads the
	// compiled module into the local cache, and executes it.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--log-level", "wasm:info",
		"--extension", "wasm-e2e:0.1.0",
	)

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			// No --config was passed, so the plugin uses its default header value.
			return r.Header.Get("x-wasm-header") == "example"
		})
}

func TestLocalGoExtension(t *testing.T) {
	testLocalGoExtension(t, false)
}

func TestLocalGoExtensionLegacyPluginPath(t *testing.T) {
	testLocalGoExtension(t, true)
}

func testLocalGoExtension(t *testing.T, removeCSharedMain bool) {
	t.Helper()
	testRegistry.Configure(t)

	// Load composer version to make it explicit in the create command and avoid pulling it from the
	// public extension catalog, as versions may differ with the local one.
	manifests, err := extensions.LoadManifests(internaltesting.ExtensionsFS(t), ".", false)
	require.NoError(t, err)
	composer, ok := manifests[extensions.ComposerArtifact]
	require.True(t, ok)

	// Local builds for Go will pull libcomposer from the remote registry. However, when we're doing changes to versions, etc, we don't want it to
	// pull an obsolete version, so we'll just push the current composer source to the local registry and use that for the test.
	makeCmd := exec.CommandContext(t.Context(), "make", "push_code")
	makeCmd.Dir = "../../extensions/composer"
	output, err := makeCmd.CombinedOutput()
	t.Logf("make push_code output: %s", string(output))
	require.NoError(t, err)

	dataDir := t.TempDir()

	// Create a brand new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "go-e2e", "--path", dataDir, "--composer-version", composer.Version)
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

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			// For the response, the execution order of the extensions is in reverse order of the
			// declaration order, so the header from the second extension should come first.
			headerValues, ok := r.Header[http.CanonicalHeaderKey("x-go-e2e")]
			return ok && len(headerValues) == 2 &&
				headerValues[0] == "configured-value" &&
				headerValues[1] == "example"
		})
}

func addDummyDependencyToExtension(t *testing.T, path string) {
	// Create another module in the extension project as a dummy dependency for the extension.
	// This is to test that the extension can be built and run successfully even the dependencies
	// of the extension are not subset of the composer's dependencies.

	goModContent := `module inner
go %s
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
	err = os.WriteFile(goModPath, []byte(fmt.Sprintf(goModContent, internal.GoVersion)), 0o600)
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

// TestGoPluginLoaderRemoteExtension exercises the raw goplugin-loader extension end to end:
//  1. build the example Go plugin image and push it to the local test registry;
//  2. build the full composer image and push it to the local test registry, so boe downloads
//     libcomposer.so (the composer bundle that hosts the built-in goplugin-loader) from there
//     into the local cache and resolves the embedded goplugin-loader manifest;
//  3. run `boe run --extension goplugin-loader --config '{"name":...,"url":"oci://..."}'`
//     and assert the dynamically loaded plugin processes responses.
//
// Both the plugin and libcomposer.so are built from the composer Dockerfiles, which pin the
// same GO_VERSION from go.mod. This matters because plugin.Open requires the plugin and host
// to share an identical Go toolchain and dependency set; strict_check=false only relaxes the
// soft build-info checks, not the linker's hard ABI requirement.
func TestGoPluginLoaderRemoteExtension(t *testing.T) {
	// Building two images, pushing, and starting Envoy can take a while.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	const composerDir = "../../extensions/composer"

	// The composer (and thus the example plugin) version comes from the composer manifest.
	manifests, err := extensions.LoadManifests(internaltesting.ExtensionsFS(t), ".", false)
	require.NoError(t, err)
	composer, ok := manifests[extensions.ComposerArtifact]
	require.True(t, ok)
	version := composer.Version

	// Point both the build tooling and the goplugin-loader image fetcher at the local
	// insecure registry. These env vars are inherited by the spawned boe process, so the
	// fetcher pulls the plugin over plain HTTP (BOE_REGISTRY_INSECURE).
	testRegistry.Configure(t)

	// Dedicated data home so the composer bundle download and the plugin pull cache are isolated.
	dataHome := t.TempDir()
	t.Setenv("BOE_DATA_HOME", dataHome)

	// Step 1: build the example Go plugin image for the local platform and push it to the local
	// registry via the Makefile's push_image target. PLATFORMS=linux/<arch> selects a single-platform
	// export, for which push_image emits manifest-level OCI annotations (no index annotations, which
	// buildkit rejects for single-platform). The target pushes directly (--output type=registry), so
	// no separate docker push is needed. It tags the image <HUB>/extension-<name>:<version>, with HUB
	// derived from OCI_REGISTRY; BOE_REGISTRY_INSECURE (set above) makes the export insecure.
	pluginRef := fmt.Sprintf("%s/built-on-envoy/extension-example-go:%s", testRegistry.Address, version)
	runCmd(t, composerDir, "make", "-f", "Makefile.plugin", "push_image",
		"PLATFORMS=linux/"+runtime.GOARCH,
		"EXTENSION_PATH=example",
		"OCI_REGISTRY="+testRegistry.Address,
	)
	pluginURL := "oci://" + pluginRef

	// Step 2: build the full composer image and push it to the local registry (as
	// <registry>/composer:<version>). When the goplugin-loader runs in Step 3, boe downloads
	// libcomposer.so from there into <dataHome>/extensions/dym/composer/<version>/libcomposer.so
	// and resolves the embedded goplugin-loader manifest from the bundle's /metadatas.
	runCmd(t, composerDir, "make", "push_image",
		"PLATFORMS=linux/"+runtime.GOARCH,
		"OCI_REGISTRY="+testRegistry.Address,
	)

	// Step 3: run the goplugin-loader extension, pointing it at the pushed plugin image via
	// the user-supplied URL.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	config := fmt.Sprintf(`{"name":"example-go","url":%q,"strict_check":false}`, pluginURL)
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--envoy-version", "dev-latest",
		"--log-level", "dynamic_modules:debug",
		"--extension", extensions.GoPluginLoaderName+":"+version,
		"--config", config,
	)

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-example-response-header") == "example-value"
		})
}

// TestComposerBundleExtension exercises the bundle-prefixed extension syntax end to end:
//  1. build the full composer image (non-lite, single platform) and push it to the local test registry;
//  2. run `boe run --extension composer/example-go` which downloads the full composer binary
//     artifact and resolves the example-go child extension manifest from within the bundle.
func TestComposerBundleExtension(t *testing.T) {
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	const composerDir = "../../extensions/composer"

	manifests, err := extensions.LoadManifests(internaltesting.ExtensionsFS(t), ".", false)
	require.NoError(t, err)
	composer, ok := manifests[extensions.ComposerArtifact]
	require.True(t, ok)
	version := composer.Version

	testRegistry.Configure(t)
	t.Setenv("BOE_DATA_HOME", t.TempDir())

	// Build and push the full composer image (single platform) to the local registry.
	runCmd(t, composerDir, "make", "push_image",
		"PLATFORMS=linux/"+runtime.GOARCH,
		"OCI_REGISTRY="+testRegistry.Address,
	)

	// Run boe with the bundle-prefixed extension reference: composer/example-go.
	// This downloads the full composer artifact keyed by the "composer" bundle name,
	// then resolves the "example-go" child extension manifest within it.
	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]
	args := []string{
		"--log-level", "dynamic_modules:debug",
		"--extension", "composer/example-go:" + version,
	}
	if strings.HasSuffix(version, "-dev") {
		args = append(args, "--dev")
	}
	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1], args...)

	internaltesting.RequireEventuallyGet(t,
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		func(r *http.Response) bool {
			return r.Header.Get("x-example-response-header") == "example-value"
		})
}

// runCmd runs name with args (in dir, if non-empty), logging the combined output and failing the
// test on a non-zero exit.
func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	// #nosec G204 -- test-controlled command and args.
	cmd := exec.CommandContext(t.Context(), name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	t.Logf("%s %s:\n%s", name, strings.Join(args, " "), out)
	require.NoError(t, err)
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
