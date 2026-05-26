// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestCreateGoWithDockerSupport(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := t.Context()

	t.Setenv("BOE_REGISTRY_INSECURE", "true")
	t.Setenv("BOE_REGISTRY", registryAddr)

	// Create a new extension with an explicit composer version to avoid registry lookup in tests.
	version := "0.1.0"
	process := internaltesting.RunCLI(t, cliBin, "create", "test-docker", "--path", tmpDir,
		"--composer-version", version)
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	extensionDir := filepath.Join(tmpDir, "test-docker")
	// Create a dummy config.schema.json to test it gets included in the download for Go extensions.
	require.NoError(t, os.WriteFile(filepath.Join(extensionDir, "config.schema.json"), []byte(`{}`), 0o600))

	t.Run("cshared", func(t *testing.T) {
		t.Run("makefile_build", func(t *testing.T) {
			// #nosec G204
			makeCmd := exec.CommandContext(ctx, "make", "build")
			makeCmd.Dir = extensionDir
			output, err := makeCmd.CombinedOutput()
			t.Logf("make build output: %s", string(output))
			require.NoError(t, err, "Makefile build target should be valid")

			// List the extension directory after build to check for output files
			files, err := os.ReadDir(extensionDir)
			require.NoError(t, err, "Should be able to read extension directory after build")
			var fileNames []string
			for _, file := range files {
				fileNames = append(fileNames, file.Name())
			}
			t.Logf("Files in extension directory after build: %v", fileNames)

			// Check there is the c-shared library output file (lib<name>.so).
			_, fileCheckError := os.Stat(filepath.Join(extensionDir, "libtest-docker.so"))
			if fileCheckError != nil {
				t.Errorf("Makefile build target should produce libtest-docker.so, but got error: %v", fileCheckError)
			}
		})

		t.Run("makefile_build_image", func(t *testing.T) {
			internaltesting.MaybeSkipLongRunningTest(t)
			// #nosec G204
			makeCmd := exec.CommandContext(ctx, "make", "build_image")
			makeCmd.Dir = extensionDir
			output, err := makeCmd.CombinedOutput()
			t.Logf("make build_image output: %s", string(output))
			require.NoError(t, err, "Makefile build_image target should be valid")

			manifest := &extensions.Manifest{Name: "test-docker", Version: version, Type: extensions.TypeGo, CShared: true}
			pushOCIImageForDownload(t, extensionDir, "./Dockerfile", manifest)
			requireDownloadHasFiles(t, manifest, "manifest.yaml", "config.schema.json")
		})
	})

	t.Run("goplugin", func(t *testing.T) {
		t.Run("makefile_build_plugin", func(t *testing.T) {
			// #nosec G204
			makeCmd := exec.CommandContext(ctx, "make", "build-plugin")
			makeCmd.Dir = extensionDir
			output, err := makeCmd.CombinedOutput()
			t.Logf("make build_plugin output: %s", string(output))
			require.NoError(t, err, "Makefile build_plugin target should be valid")

			// List the extension directory after build to check for output files
			files, err := os.ReadDir(extensionDir)
			require.NoError(t, err, "Should be able to read extension directory after build")
			var fileNames []string
			for _, file := range files {
				fileNames = append(fileNames, file.Name())
			}
			t.Logf("Files in extension directory after build: %v", fileNames)

			// Check there is the plugin output file (plugin.so).
			_, fileCheckError := os.Stat(filepath.Join(extensionDir, "plugin.so"))
			if fileCheckError != nil {
				t.Errorf("Makefile build_plugin target should produce plugin.so, but got error: %v", fileCheckError)
			}
		})

		t.Run("makefile_build_image_plugin", func(t *testing.T) {
			internaltesting.MaybeSkipLongRunningTest(t)
			// #nosec G204
			makeCmd := exec.CommandContext(ctx, "make", "build_image_plugin")
			makeCmd.Dir = extensionDir
			output, err := makeCmd.CombinedOutput()
			t.Logf("make build_image_plugin output: %s", string(output))
			require.NoError(t, err, "Makefile build_image_plugin target should be valid")

			manifest := &extensions.Manifest{Name: "test-docker", Version: version, Type: extensions.TypeGo, CShared: false}
			pushOCIImageForDownload(t, extensionDir, "./Dockerfile.plugin", manifest)
			requireDownloadHasFiles(t, manifest, "manifest.yaml", "config.schema.json")
		})
	})

	t.Run("makefile_code_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_code")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_code output: %s", string(output))
		require.NoError(t, err, "Makefile push_code target should be valid")

		// Pull the image manifest and check annotations
		fetchManifest(t, registryAddr, "extension-src-test-docker", version)
	})
}

func TestCreateRustWithDockerSupport(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := t.Context()

	t.Setenv("BOE_REGISTRY_INSECURE", "true")
	t.Setenv("BOE_REGISTRY", registryAddr)

	// Create a new extension
	process := internaltesting.RunCLI(t, cliBin, "create", "test-docker", "--path", tmpDir,
		"--type", "rust")
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	version := "0.1.0"
	extensionDir := filepath.Join(tmpDir, "test-docker")
	// Create a dummy config.schema.json to test it gets included in the download for Go extensions.
	require.NoError(t, os.WriteFile(filepath.Join(extensionDir, "config.schema.json"), []byte(`{}`), 0o600))

	t.Run("makefile_image_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "build_image")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make build_image output: %s", string(output))
		require.NoError(t, err, "Makefile build_image target should be valid")

		// Pull the image manifest and check contents
		manifest := &extensions.Manifest{Name: "test-docker", Version: version, Type: extensions.TypeRust}
		pushOCIImageForDownload(t, extensionDir, "./Dockerfile", manifest)
		requireDownloadHasFiles(t, manifest, "manifest.yaml", "config.schema.json")
	})

	t.Run("makefile_code_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_code")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_code output: %s", string(output))
		require.NoError(t, err, "Makefile push_code target should be valid")

		// Pull the image manifest and check annotations
		fetchManifest(t, registryAddr, "extension-src-test-docker", version)
	})
}

func TestCreateRustNetworkWithDockerSupport(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := t.Context()

	t.Setenv("BOE_REGISTRY_INSECURE", "true")
	t.Setenv("BOE_REGISTRY", registryAddr)

	// Create a new rust network filter extension
	process := internaltesting.RunCLI(t, cliBin, "create", "test-docker-network",
		"--path", tmpDir, "--type", "rust", "--filter-type", "network")
	status, err := process.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode())

	extensionDir := filepath.Join(tmpDir, "test-docker-network")
	version := "0.1.0"

	manifest := &extensions.Manifest{
		Name:       "test-docker-network",
		Version:    version,
		Type:       extensions.TypeRust,
		FilterType: extensions.FilterTypeNetwork,
	}

	t.Run("makefile_image_target", func(t *testing.T) {
		// Building the image compiles the scaffolded Rust network filter
		// project end-to-end, catching template regressions that unit tests
		// (which only check file contents) can't catch.
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "build_image")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make build_image output: %s", string(output))
		require.NoError(t, err, "Makefile build_image target should be valid")

		// Push local image to registry and check its annotations
		pushOCIImageForDownload(t, extensionDir, "./Dockerfile", manifest)
		requireDownloadHasFiles(t, manifest, "manifest.yaml")
	})

	t.Run("makefile_code_target", func(t *testing.T) {
		// #nosec G204
		makeCmd := exec.CommandContext(ctx, "make", "push_code")
		makeCmd.Dir = extensionDir
		output, err := makeCmd.CombinedOutput()
		t.Logf("make push_code output: %s", string(output))
		require.NoError(t, err, "Makefile push_code target should be valid")

		// Pull the image manifest and check annotations
		fetchManifest(t, registryAddr, "extension-src-test-docker-network", version)
	})
}

// pushOCIImageForDownload pushes the extension image to the test registry in OCI format,
// preserving the manifest annotations required by boe download.
// It does not use the `make push_image` target because it is meant for multi-platform builds
// and here we want to build for a single platform. We call directly buildx build with the
// minimum set of annotations and config.
func pushOCIImageForDownload(t *testing.T, extensionDir, dockerfile string, manifest *extensions.Manifest) {
	t.Helper()
	ctx := t.Context()

	// Ensure boe-builder exists; the Makefile's create_builder target is idempotent.
	// #nosec G204
	createBuilderCmd := exec.CommandContext(ctx, "make", "create_builder")
	createBuilderCmd.Dir = extensionDir
	out, err := createBuilderCmd.CombinedOutput()
	t.Logf("make create_builder output: %s", string(out))
	require.NoError(t, err, "Should be able to ensure boe-builder exists")

	args := []string{
		"buildx", "build",
		"--builder", "boe-builder",
		"--platform", fmt.Sprintf("linux/%s", runtime.GOARCH),
		"--output", "type=registry,oci-mediatypes=true,registry.insecure=true",
		"--provenance=false",
		"--tag", fmt.Sprintf("%s/extension-%s:%s", registryAddr, manifest.Name, manifest.Version),
		"-f", dockerfile,
		"--annotation", fmt.Sprintf("manifest:org.opencontainers.image.title=%s", manifest.Name),
		"--annotation", fmt.Sprintf("manifest:org.opencontainers.image.version=%s", manifest.Version),
		"--annotation", fmt.Sprintf("manifest:%s=%s", extensions.OCIAnnotationExtensionType, string(manifest.Type)),
		"--annotation", fmt.Sprintf("manifest:%s=%s", extensions.OCIAnnotationCShared, strconv.FormatBool(manifest.CShared)),
	}
	if manifest.Type == extensions.TypeGo {
		args = append(args, "--build-arg", fmt.Sprintf("NAME=%s", manifest.Name))
	}
	if manifest.FilterType != "" {
		args = append(args, "--annotation", fmt.Sprintf("manifest:%s=%s", extensions.OCIAnnotationFilterType, string(manifest.FilterType)))
	}
	args = append(args, ".")

	// #nosec G204
	pushCmd := exec.CommandContext(ctx, "docker", args...)
	pushCmd.Dir = extensionDir
	output, err := pushCmd.CombinedOutput()
	t.Logf("pushOCIImageForDownload output: %s", string(output))
	require.NoError(t, err, "Should be able to push OCI image to local registry")
}

func fetchManifest(t *testing.T, registry, repository, reference string) {
	url := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repository, reference)
	// Set Accept header to request OCI media types to ensure the manifest is in OCI format
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err, "failed to create HTTP request for manifest")
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err, "failed to get manifest from %s", url)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status code %d fetching manifest from %s: %s", resp.StatusCode, url, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read manifest body")
	t.Logf("Manifest for %s:%s: %s", repository, reference, string(body))
}
