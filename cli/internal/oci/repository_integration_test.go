// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package oci

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func init() {
	repoClientTests = append(repoClientTests, repoClientTest{
		name:          "docker registry",
		newRepoClient: newLocalRegistryRepositoryClient,
	})
}

func newLocalRegistryRepositoryClient(t *testing.T) RepositoryClient {
	// Create a remote repository and client
	repoRef := fmt.Sprintf("%s/test/extension", registryAddr)
	repo, err := NewRemoteRepository(repoRef, &ClientOptions{
		PlainHTTP: true, // Local registry doesn't use TLS
	})
	require.NoError(t, err)

	return NewRepositoryClient(repo)
}

func TestPullMultiArch(t *testing.T) {
	internaltesting.CreateBuildxBuilder(t)

	// Create the multi-arch image
	testRepo := fmt.Sprintf("%s/test/multiarch", registryAddr)
	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "docker", "buildx", "build",
		"--platform", "linux/amd64,linux/arm64",
		"-t", testRepo+":latest",
		"--push",
		"testdata",
	)
	output, err := cmd.CombinedOutput()
	t.Logf("docker buildx output: %s", string(output))
	require.NoError(t, err)

	repo, err := NewRemoteRepository(testRepo, &ClientOptions{PlainHTTP: true})
	require.NoError(t, err)
	client := NewRepositoryClient(repo)

	tests := []struct {
		name     string
		platform *ocispec.Platform
		wantErr  error
	}{
		{
			name:     "linux/amd64",
			platform: &ocispec.Platform{OS: "linux", Architecture: "amd64"},
		},
		{
			name:     "linux/arm64",
			platform: &ocispec.Platform{OS: "linux", Architecture: "arm64"},
		},
		{
			name:     "darwin/arm64 (unsupported platform)",
			platform: &ocispec.Platform{OS: "darwin", Architecture: "arm64"},
			wantErr:  ErrPlatformNotFound,
		},
	}

	// Pull each multiarch image and verify that it only contains the expected file.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dest := t.TempDir()

			manifest, _, err := client.Pull(t.Context(), "latest", dest, tc.platform)
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr != nil {
				return
			}

			require.NotNil(t, manifest)

			// Verify that only the expected file for the platform is present in the pulled directory.
			require.DirExists(t, dest+"/files")
			files, err := os.ReadDir(dest + "/files")
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, fmt.Sprintf("test-%s-%s.txt",
				tc.platform.OS, tc.platform.Architecture), files[0].Name())
		})
	}
}
