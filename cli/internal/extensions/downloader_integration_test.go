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

var (
	registryAddr string
	builder      string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	registryContainer, addr, err := internaltesting.StartOCIRegistry(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start local OCI registry: %v\n", err)
		os.Exit(1)
	}
	registryAddr = addr

	var cleanup func()
	builder, cleanup, err = internaltesting.CreateBuildxBuilder(ctx)
	if err != nil {
		_ = registryContainer.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "failed to create buildx builder: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = registryContainer.Terminate(ctx)
	cleanup()

	os.Exit(code)
}

func TestNewOCIRepositoryClient(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	t.Run("missing repo", func(t *testing.T) {
		client, err := newOCIRepositoryClient(logger, registryAddr, "", "", true)
		require.Error(t, err)
		require.Nil(t, client)
	})

	t.Run("can connect", func(t *testing.T) {
		client, err := newOCIRepositoryClient(logger, registryAddr+"/repo", "", "", true)
		require.NoError(t, err)
		// We just want to test connectivity here so we expect a failure saying that
		// the repo does not exist, but this is good enough for this test.
		_, err = client.Tags(t.Context())
		require.ErrorContains(t, err, "repository name not known")
	})
}

func TestDownloadExtensionWithManifest(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

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
		Logger:   logger,
		Registry: registryAddr,
		Dirs:     &xdg.Directories{DataHome: t.TempDir()},
		Insecure: true,
		OS:       "linux",
		Arch:     "amd64",
	}

	artifact, err := downloader.DownloadExtension(t.Context(), "test", "latest")
	require.NoError(t, err)
	require.True(t, artifact.Manifest.Remote)
	// Verify that the manifest has been read by checking properties that are
	// not present in OCI annotations.
	require.NotNil(t, artifact.Manifest.Lua)
	require.Equal(t, "test.lua", artifact.Manifest.Lua.Path)
}

func TestCheckOrDownloadComposer(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

	// Create the image simulating to be composer
	composerRepo := fmt.Sprintf("%s/composer-lite", registryAddr)
	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"--platform", "linux/amd64",
		"-t", composerRepo+":1.2.3",
		"--push",
		"--annotation", "index,manifest:org.opencontainers.image.title=composer-lite",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.composer_version=1.2.3",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.artifact=binary",
		"testdata/extension-with-manifest",
	)
	output, err := cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	downloader := &Downloader{
		Logger:   logger,
		Registry: registryAddr,
		Dirs:     &xdg.Directories{DataHome: t.TempDir()},
		Insecure: true,
		OS:       "linux",
		Arch:     "amd64",
	}

	assert.ErrorContains(t, CheckOrDownloadLibComposer(t.Context(), downloader, "unexisting", false), "not found") //nolint: testifylint
	assert.NoError(t, CheckOrDownloadLibComposer(t.Context(), downloader, "1.2.3", false))                         //nolint: testifylint
	assert.FileExists(t, LocalCacheComposerLib(downloader.Dirs, "1.2.3"))                                          //nolint: testifylint
}

func TestFallbackToSourceDynamicModule(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

	// Push a platform-specific image
	testRepo := fmt.Sprintf("%s/extension-test-dynamic-module", registryAddr)
	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"--platform", "linux/amd64",
		"-t", testRepo+":latest",
		"--push",
		"--annotation", "index,manifest:org.opencontainers.image.title=test-dynamic-module",
		"--annotation", "index,manifest:org.opencontainers.image.version=1.0.0",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.type=rust",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.artifact=binary",
		"testdata/extension-binary",
	)
	output, err := cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	downloader := &Downloader{
		Logger:   logger,
		Registry: registryAddr,
		Dirs:     &xdg.Directories{DataHome: t.TempDir()},
		Insecure: true,
		OS:       "darwin",
		Arch:     "amd64",
	}

	// If the source artifact is not there, the download will fail because no artifact is there
	// for the given platform
	_, err = downloader.DownloadExtension(t.Context(), "test-dynamic-module", "latest")
	require.Error(t, err)

	// Push the source image
	testSrcRepo := fmt.Sprintf("%s/extension-src-test-dynamic-module", registryAddr)
	// #nosec G204
	cmd = exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"-t", testSrcRepo+":latest",
		"--push",
		"--provenance=false",
		"--annotation", "org.opencontainers.image.title=test-dynamic-module",
		"--annotation", "org.opencontainers.image.version=1.0.0",
		"--annotation", "io.tetratelabs.built-on-envoy.extension.type=rust",
		"--annotation", "io.tetratelabs.built-on-envoy.extension.artifact=source",
		"testdata/extension-src",
	)
	output, err = cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	// Now the download would succeed and fallback to the source artifact
	artifact, err := downloader.DownloadExtension(t.Context(), "test-dynamic-module", "latest")
	require.NoError(t, err)
	require.True(t, artifact.Manifest.Remote)
	require.Equal(t, ArtifactSource, artifact.ArtifactType)
	require.FileExists(t, artifact.Path+"/lib.rs")
	require.NoFileExists(t, artifact.Path+"/libplugin.so")
}

func TestFallbackToSourceComposer(t *testing.T) {
	logger := internaltesting.NewTLogger(t)

	// Push a platform-specific image
	testRepo := fmt.Sprintf("%s/extension-test-composer", registryAddr)
	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"--platform", "linux/amd64",
		"-t", testRepo+":latest",
		"--push",
		"--annotation", "index,manifest:org.opencontainers.image.title=test-composer",
		"--annotation", "index,manifest:org.opencontainers.image.version=1.0.0",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.composer_version=1.0.0",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.type=go",
		"--annotation", "index,manifest:io.tetratelabs.built-on-envoy.extension.artifact=binary",
		"testdata/composer-binary",
	)
	output, err := cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	downloader := &Downloader{
		Logger:   logger,
		Registry: registryAddr,
		Dirs:     &xdg.Directories{DataHome: t.TempDir()},
		Insecure: true,
		OS:       "darwin",
		Arch:     "amd64",
	}

	// If the source artifact is not there, the download will fail because no artifact is there
	// for the given platform
	_, err = downloader.DownloadExtension(t.Context(), "test-composer", "latest")
	require.Error(t, err)

	// Push the source image
	testSrcRepo := fmt.Sprintf("%s/composer-src", registryAddr)
	// #nosec G204
	cmd = exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--builder", builder,
		"-t", testSrcRepo+":1.0.0",
		"--push",
		"--provenance=false",
		"--annotation", "org.opencontainers.image.title=composer",
		"--annotation", "org.opencontainers.image.version=1.0.0",
		"--annotation", "io.tetratelabs.built-on-envoy.extension.type=composer",
		"--annotation", "io.tetratelabs.built-on-envoy.extension.artifact=source",
		"testdata/composer-src",
	)
	output, err = cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	// Now the download would succeed and fallback to the source artifact
	artifact, err := downloader.DownloadExtension(t.Context(), "test-composer", "latest")
	require.NoError(t, err)
	require.True(t, artifact.Manifest.Remote)
	require.Equal(t, ArtifactSource, artifact.ArtifactType)
	require.FileExists(t, artifact.Path+"/libcomposer.go")
	require.NoFileExists(t, artifact.Path+"/libcomposer.so")
}
