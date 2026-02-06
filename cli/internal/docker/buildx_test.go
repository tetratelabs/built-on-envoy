// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package docker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestDetectGitInfo(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		dir         string
		expectEmpty bool
	}{
		{
			name:        "git repository (current directory)",
			dir:         ".",
			expectEmpty: false,
		},
		{
			name:        "non-git directory",
			dir:         "/tmp",
			expectEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := DetectGitInfo(ctx, tt.dir)
			require.NoError(t, err)
			require.NotNil(t, info)

			if tt.expectEmpty {
				// Non-git directories should have empty fields
				require.Empty(t, info.RemoteURL)
				require.Empty(t, info.CommitSHA)
			} else {
				// Git repository should have at least commit SHA
				// (remote URL might be empty for some repos)
				t.Logf("Git info detected - Remote: %s, Commit: %s", info.RemoteURL, info.CommitSHA)
			}
		})
	}
}

func TestCreateAndRemoveBuilder(t *testing.T) {
	ctx := context.Background()

	// Skip if Docker not available
	if err := CheckDockerBuildx(ctx); err != nil {
		t.Skipf("Docker buildx not available: %v", err)
	}

	// Create a test builder
	testBuilderName := "boe-test-builder-" + t.Name()

	// Create builder
	err := checkOrCreateBuilder(ctx, testBuilderName)
	require.NoError(t, err)

	// Cleanup
	defer func() {
		_ = removeBuilder(ctx, testBuilderName)
	}()

	// Remove builder
	err = removeBuilder(ctx, testBuilderName)
	require.NoError(t, err)
}
