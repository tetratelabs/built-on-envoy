// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package extensions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

var registryAddr string

func TestMain(m *testing.M) {
	ctx := context.Background()
	registryContainer, addr, err := internaltesting.StartOCIRegistry(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start local OCI registry: %v\n", err)
		os.Exit(1)
	}
	registryAddr = addr

	code := m.Run()
	_ = registryContainer.Terminate(ctx)
	os.Exit(code)
}

func TestNewOCIRepositoryClient(t *testing.T) {
	t.Run("missing repo", func(t *testing.T) {
		client, err := newOCIRepositoryClient(registryAddr, "", "", true)
		require.Error(t, err)
		require.Nil(t, client)
	})

	t.Run("can connect", func(t *testing.T) {
		client, err := newOCIRepositoryClient(registryAddr+"/repo", "", "", true)
		require.NoError(t, err)
		// We just want to test connectivity here so we expect a failure saying that
		// the repo does not exist, but this is good enough for this test.
		_, err = client.Tags(t.Context())
		require.ErrorContains(t, err, "repository name not known")
	})
}

func TestDownloadExtensionWithManifest(t *testing.T) {
	builder := internaltesting.CreateBuildxBuilder(t)

	// Create the image
	testRepo := fmt.Sprintf("%s/extension-test", registryAddr)
	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"--platform", "linux/amd64",
		"-t", testRepo+":latest",
		"--push",
		"--annotation", "index,manifest:org.opencontainers.image.title=test",
		"--annotation", "index,manifest:org.opencontainers.image.version=1.0.0",
		"testdata/extension-with-manifest",
	)
	output, err := cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	downloader := &Downloader{
		Registry: registryAddr,
		Dirs:     &xdg.Directories{DataHome: t.TempDir()},
		Insecure: true,
		OS:       "linux",
		Arch:     "amd64",
	}

	manifest, err := downloader.DownloadExtension(t.Context(), "test", "latest")
	require.NoError(t, err)
	// Verify that the manifest has been read by checking properties that are
	// not present in OCI annotations.
	require.NotNil(t, manifest.Lua)
	require.Equal(t, "test.lua", manifest.Lua.Path)
}

func TestCheckOrDownloadComposer(t *testing.T) {
	builder := internaltesting.CreateBuildxBuilder(t)

	// Create the image simulating to be composer
	composerRepo := fmt.Sprintf("%s/composer-lite", registryAddr)
	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"--platform", "linux/amd64",
		"-t", composerRepo+":1.2.3",
		"--push",
		"--annotation", "index,manifest:org.opencontainers.image.title=composer-lite",
		"--annotation", "index,manifest:org.opencontainers.image.version=1.2.3",
		"testdata/extension-with-manifest",
	)
	output, err := cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	downloader := &Downloader{
		Registry: registryAddr,
		Dirs:     &xdg.Directories{DataHome: t.TempDir()},
		Insecure: true,
		OS:       "linux",
		Arch:     "amd64",
	}

	assert.ErrorContains(t, CheckOrDownloadLibComposer(t.Context(), downloader, "unexisting"), "not found") //nolint: testifylint
	assert.NoError(t, CheckOrDownloadLibComposer(t.Context(), downloader, "1.2.3"))                         //nolint: testifylint
	assert.FileExists(t, LocalCacheComposerLib(downloader.Dirs, "1.2.3"))                                   //nolint: testifylint
}
