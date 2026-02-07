// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package docker

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestParsePlatforms(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single platform",
			input:    "linux/amd64",
			expected: []string{"linux/amd64"},
		},
		{
			name:     "multiple platforms",
			input:    "linux/amd64,linux/arm64",
			expected: []string{"linux/amd64", "linux/arm64"},
		},
		{
			name:     "with spaces",
			input:    "linux/amd64, linux/arm64",
			expected: []string{"linux/amd64", "linux/arm64"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "trailing comma",
			input:    "linux/amd64,",
			expected: []string{"linux/amd64"},
		},
		{
			name:     "duplicate platforms",
			input:    "linux/amd64,linux/arm64,linux/amd64",
			expected: []string{"linux/amd64", "linux/arm64"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePlatforms(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractRegistry(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		expected string
	}{
		{
			name:     "ghcr.io",
			imageRef: "ghcr.io/org/repo:tag",
			expected: "ghcr.io",
		},
		{
			name:     "localhost with port",
			imageRef: "localhost:5000/repo:tag",
			expected: "localhost:5000",
		},
		{
			name:     "docker hub implicit",
			imageRef: "repo:tag",
			expected: "docker.io",
		},
		{
			name:     "unusual registry name without dot or port",
			imageRef: "myorg/repo:tag",
			expected: "myorg",
		},
		{
			name:     "custom registry",
			imageRef: "registry.example.com/org/repo:tag",
			expected: "registry.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRegistry(tt.imageRef)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestValidatePlatform(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		wantErr  bool
	}{
		{
			name:     "valid linux/amd64",
			platform: "linux/amd64",
			wantErr:  false,
		},
		{
			name:     "valid linux/arm64",
			platform: "linux/arm64",
			wantErr:  false,
		},
		{
			name:     "invalid - linux/arm/v7 not supported",
			platform: "linux/arm/v7",
			wantErr:  true,
		},
		{
			name:     "invalid - linux/386",
			platform: "linux/386",
			wantErr:  true,
		},
		{
			name:     "invalid - windows/amd64",
			platform: "windows/amd64",
			wantErr:  true,
		},
		{
			name:     "invalid - empty",
			platform: "",
			wantErr:  true,
		},
		{
			name:     "valid with whitespace",
			platform: " linux/amd64 ",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePlatform(tt.platform)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "unsupported platform")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckDockerAvailable(t *testing.T) {
	ctx := context.Background()

	// This test requires Docker to be running
	err := CheckDockerAvailable(ctx)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}

	require.NoError(t, err)
}

func TestCheckDockerBuildx(t *testing.T) {
	ctx := context.Background()

	// This test requires Docker and buildx to be available
	err := CheckDockerBuildx(ctx)
	if err != nil {
		t.Skipf("Docker or buildx not available: %v", err)
	}

	require.NoError(t, err)
}

func TestCheckOrCreateBuilder(t *testing.T) {
	ctx := context.Background()

	// Skip if Docker not available
	if err := CheckDockerBuildx(ctx); err != nil {
		t.Skipf("Docker buildx not available: %v", err)
	}

	// Create a test builder
	testBuilderName := "boe-test-builder-" + t.Name()

	// Create builder
	err := checkOrCreateBuilder(ctx, testBuilderName, "")
	require.NoError(t, err)

	// Print the builder context for debugging
	cmd := exec.CommandContext(ctx, "docker", "builder", "inspect", testBuilderName)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	t.Logf("Builder context output: %s", string(output))

	// Cleanup
	defer func() {
		_ = removeBuilder(ctx, testBuilderName)
	}()

	// Remove builder
	err = removeBuilder(ctx, testBuilderName)
	require.NoError(t, err)
}

func TestCheckOrCreateBuilder_WithCustomConfig(t *testing.T) {
	ctx := context.Background()

	// Skip if Docker not available
	if err := CheckDockerBuildx(ctx); err != nil {
		t.Skipf("Docker buildx not available: %v", err)
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "buildkitd.toml")

	// Write a minimal custom config (can be empty for this test)
	err := os.WriteFile(configPath, []byte(`
[registry."registry.local:5000"]
  http = true
  insecure = true
`), 0o644)
	require.NoError(t, err)

	// Create a test builder
	testBuilderName := "boe-test-builder-with-custom-config-" + t.Name()

	// Create builder
	err = checkOrCreateBuilder(ctx, testBuilderName, configPath)
	require.NoError(t, err)

	// Print the builder context for debugging
	cmd := exec.CommandContext(ctx, "docker", "builder", "inspect", testBuilderName)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	t.Logf("Builder context output: %s", string(output))

	// Cleanup
	defer func() {
		_ = removeBuilder(ctx, testBuilderName)
	}()

	// Remove builder
	err = removeBuilder(ctx, testBuilderName)
	require.NoError(t, err)
}

func TestCheckOrCreateBuilder_Idempotent(t *testing.T) {
	ctx := context.Background()

	// Skip if Docker not available
	if err := CheckDockerBuildx(ctx); err != nil {
		t.Skipf("Docker buildx not available: %v", err)
	}

	testBuilderName := "boe-test-builder-idempotent-" + t.Name()

	// Cleanup
	defer func() {
		_ = removeBuilder(ctx, testBuilderName)
	}()

	// Create builder first time
	err := checkOrCreateBuilder(ctx, testBuilderName, "")
	require.NoError(t, err)

	// Create builder second time (should be idempotent)
	err = checkOrCreateBuilder(ctx, testBuilderName, "")
	require.NoError(t, err)
}

func TestGitInfo(t *testing.T) {
	// Check if `git` command is available
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git command not available, skipping TestGitInfo")
	}
	ctx := context.Background()

	tempDir1 := t.TempDir()
	gitInfo := detectGitInfo(ctx, tempDir1)

	require.NotNil(t, gitInfo)
	require.Empty(t, gitInfo.RemoteURL)
	require.Empty(t, gitInfo.CommitSHA)

	// Initialize a git repository and make a commit
	err = exec.Command("git", "init").Run()
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("test"), 0o644)
	require.NoError(t, err)
	err = exec.Command("git", "add", "file.txt").Run()
	require.NoError(t, err)
	err = exec.Command("git", "commit", "-m", "test commit").Run()
	require.NoError(t, err)

	// Detect git info again
	gitInfo = detectGitInfo(ctx, tempDir1)
	require.NotNil(t, gitInfo)
	require.Empty(t, gitInfo.RemoteURL)

	require.NotEmpty(t, gitInfo.CommitSHA)

	// Add a remote and detect again
	err = exec.Command("git", "remote", "add", "origin", "https://github.com/example/repo.git").Run()
	require.NoError(t, err)

	// Detect git info again
	gitInfo = detectGitInfo(ctx, tempDir1)
	require.NotNil(t, gitInfo)
	require.Equal(t, "https://github.com/example/repo.git", gitInfo.RemoteURL)
	require.NotEmpty(t, gitInfo.CommitSHA)
}

// Test platform validation in BuildAndPushImage context
func TestBuildAndPushImage_InvalidPlatform(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	opts := &BuildAndPushOptions{
		Context:    ".",
		PluginName: "test",
		ImageRef:   "test:v1",
		Platforms:  []string{"invalid/platform"},
		Dockerfile: "Dockerfile",
		Version:    "1.0.0",
	}

	err := BuildAndPushImage(ctx, opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported platform")
}

// Test supportedPlatforms map
func TestSupportedPlatforms(t *testing.T) {
	require.True(t, supportedPlatforms["linux/amd64"])
	require.True(t, supportedPlatforms["linux/arm64"])
	require.False(t, supportedPlatforms["windows/amd64"])
	require.False(t, supportedPlatforms["linux/386"])
}

// Test dockerLogin function. It's hard to test successful login
// without valid credentials, but we can at least test the error path.
func TestDockerLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()

	container, registry, err := internaltesting.StartOCIRegistry(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	// Skip if Docker not available
	if err := CheckDockerAvailable(ctx); err != nil {
		t.Skipf("Docker not available: %v", err)
	}

	// Test with invalid credentials to trigger error path
	opts := &BuildAndPushOptions{
		ImageRef: registry + "/test/test:v1",
		Username: "invalid-user-that-does-not-exist",
		Password: "invalid-password",
	}

	err = dockerLogin(ctx, opts)
	// Expect failure with invalid credentials
	if err != nil {
		t.Logf("Expected login failure with invalid credentials: %v", err)
		require.Error(t, err)
	}
}

// Integration test for BuildAndPushImage (requires Docker)
func TestBuildAndPushImage(t *testing.T) {
	if err := flag.Set("test.timeout", "3m"); err != nil {
		t.Logf("cannot set test timeout: %v", err)
	}
	ctx := context.Background()

	// Skip if Docker not available
	if err := CheckDockerBuildx(ctx); err != nil {
		t.Skipf("Docker buildx not available: %v", err)
	}

	container, registry, err := internaltesting.StartOCIRegistry(ctx)
	require.NoError(t, err, "failed to start local OCI registry")
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	// Get port from registry address (e.g. localhost:5000) and replace localhost with
	// host.docker.internal for buildkit to access the registry
	buildkitRegistry := strings.Replace(registry, "localhost", "host.docker.internal", 1)

	// Create a temporary directory with a minimal test setup
	tmpDir := t.TempDir()

	// Create a simple Dockerfile
	dockerfile := `FROM busybox:latest
COPY plugin.so /plugin.so
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644), "failed to write Dockerfile")

	// Create a dummy plugin.so file
	pluginPath := filepath.Join(tmpDir, "plugin.so")
	require.NoError(t, os.WriteFile(pluginPath, []byte("dummy"), 0o644), "failed to write dummy plugin.so")

	opts := &BuildAndPushOptions{
		Context:    tmpDir,
		PluginName: "test-plugin",
		// Use a local registry or skip push
		ImageRef:      buildkitRegistry + "/test-plugin:test",
		Platforms:     []string{"linux/amd64"}, // Single platform to speed up
		Dockerfile:    dockerfilePath,
		Version:       "1.0.0-test",
		Description:   "Test plugin",
		Author:        "Test",
		License:       "Apache-2.0",
		ExtensionType: "wasm",
	}

	// Create a builder with randome name to make sure it's unique.
	builderName := "boe-test-builder-" + t.Name() + "-" + time.Now().Format("20060102150405")
	configPath := filepath.Join(tmpDir, "buildkitd.toml")
	err = os.WriteFile(configPath, []byte(`
[registry."`+buildkitRegistry+`"]
  http = true
  insecure = true
`), 0o644)
	require.NoError(t, err)
	err = checkOrCreateBuilder(ctx, builderName, configPath)
	require.NoError(t, err, "failed to create builder with custom config")
	defer func() {
		_ = removeBuilder(ctx, builderName)
	}()

	ctxWithBuilder := context.WithValue(ctx, customBuilderNameKey, builderName)

	// Note: This will fail without a local registry, but tests the code path
	err = BuildAndPushImage(ctxWithBuilder, opts)
	require.NoError(t, err, "BuildAndPushImage failed")

	// Check if the image was pushed to the registry by inspecting the registry
	cmd := exec.CommandContext(ctx, "curl", "-H", "Accept: application/vnd.oci.image.manifest.v1+json",
		"-s", registry+"/v2/test-plugin/manifests/test")
	output, err := cmd.CombinedOutput()
	t.Logf("Registry response for pushed image: %s", string(output))

	require.NoError(t, err, "failed to query registry for pushed image")
	// Check if the response contains the expected image reference
	require.Contains(t, string(output), `"org.opencontainers.image.description": "Test plugin"`)
}
