// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestRunner_Run_ConfigError(t *testing.T) {
	r := &RunnerFuncE{
		Logger: internaltesting.NewTLogger(t),
		Dirs: &xdg.Directories{
			DataHome: t.TempDir(),
		},
		Extensions: []*extensions.Manifest{
			{
				Name: "invalid-extension",
				Type: "unsupported-type",
			},
		},
	}

	err := r.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to generate filter config")
	require.ErrorIs(t, err, ErrUnsupportedExtensionType)
}

func TestRunner_Run_ContextCanceled(t *testing.T) {
	ext := &extensions.Manifest{
		Name: "test-lua",
		Type: extensions.TypeLua,
		Lua:  &extensions.Lua{Inline: "function envoy_on_request(request_handle) end"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	tmpdir := t.TempDir()
	r := &RunnerFuncE{
		Logger:     internaltesting.NewTLogger(t),
		Dirs:       &xdg.Directories{DataHome: tmpdir, RuntimeDir: tmpdir},
		Extensions: []*extensions.Manifest{ext},
		ListenPort: 10000,
		AdminPort:  9901,
		RunID:      "test-run",
	}

	err := r.Run(ctx)
	// Expect error because context is canceled, but we care that code was executed.
	// funce.Run typically returns error when context is canceled.
	// We mainly want to ensure no panic and that it reached funce.Run.
	if err != nil {
		assert.Contains(t, err.Error(), "context canceled")
	}
}

func TestProcessLocalExtensions(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name     string
		exts     []string
		expected []string
	}{
		{
			name:     "no local extensions",
			exts:     nil,
			expected: nil,
		},
		{
			name: "multiple extensions",
			exts: []string{"/opt/foo/ext1.so", "./ext2.so"},
			expected: []string{
				"-v", "/opt/foo/ext1.so:" + containerLocalExtensionsDir + "/ext1.so",
				"-v", filepath.Join(cwd, "ext2.so") + ":" + containerLocalExtensionsDir + "/ext2.so",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RunnerDocker{Logger: internaltesting.NewTLogger(t)}
			result, err := r.processLocalExtensions(tt.exts)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocalExtensionContainerPath(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name              string
		ext               string
		expectedAbsPath   string
		expectedContainer string
	}{
		{
			name:              "absolute path",
			ext:               "/opt/extensions/my-ext.so",
			expectedAbsPath:   "/opt/extensions/my-ext.so",
			expectedContainer: containerLocalExtensionsDir + "/my-ext.so",
		},
		{
			name:              "relative path",
			ext:               "./my-ext.so",
			expectedAbsPath:   filepath.Join(cwd, "my-ext.so"),
			expectedContainer: containerLocalExtensionsDir + "/my-ext.so",
		},
		{
			name:              "nested path uses base name only",
			ext:               "/a/deeply/nested/path/extension.so",
			expectedAbsPath:   "/a/deeply/nested/path/extension.so",
			expectedContainer: containerLocalExtensionsDir + "/extension.so",
		},
		{
			name:              "bare filename",
			ext:               "extension.so",
			expectedAbsPath:   filepath.Join(cwd, "extension.so"),
			expectedContainer: containerLocalExtensionsDir + "/extension.so",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absPath, containerPath, err := localExtensionContainerPath(tt.ext)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedAbsPath, absPath)
			assert.Equal(t, tt.expectedContainer, containerPath)
		})
	}
}

func TestProcessCommandArgs(t *testing.T) {
	args := []string{
		"boe", "run",
		"--docker", "--docker=true",
		"--pull=always", "--pull", "never",
		"--local", "/path/ext.so",
		"--local", "/host/path/ext2.so",
		"--local=./ext3.so",
		"--local=/path/ext4.so",
		"--listen-port", "8080",
	}

	want := []string{
		"run",
		"--local", containerLocalExtensionsDir + "/ext.so",
		"--local", containerLocalExtensionsDir + "/ext2.so",
		"--local=" + containerLocalExtensionsDir + "/ext3.so",
		"--local=" + containerLocalExtensionsDir + "/ext4.so",
		"--listen-port", "8080",
	}

	r := &RunnerDocker{Logger: internaltesting.NewTLogger(t)}
	result := r.processCommandArgs(args)
	assert.Equal(t, want, result)
}

func TestPassthroughEnvVars(t *testing.T) {
	// Save and restore the original environment.
	originalEnv := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, e := range originalEnv {
			k, v, _ := strings.Cut(e, "=")
			_ = os.Setenv(k, v)
		}
	})

	tests := []struct {
		name     string
		envVars  map[string]string
		expected []string
	}{
		{
			name:     "no BOE_ vars returns nil",
			envVars:  map[string]string{"HOME": "/home/user", "PATH": "/usr/bin"},
			expected: nil,
		},
		{
			name: "mix of passthrough and excluded vars",
			envVars: map[string]string{
				"BOE_REGISTRY":    "ghcr.io/test",
				"BOE_CONFIG_HOME": "/custom/config",
				"BOE_DATA_HOME":   "/custom/data",
				"BOE_STATE_HOME":  "/custom/state",
				"BOE_RUNTIME_DIR": "/custom/runtime",
				"BOE_TOKEN":       "secret",
				"HOME":            "/home/user",
			},
			expected: []string{"-e", "BOE_REGISTRY=ghcr.io/test", "-e", "BOE_TOKEN=secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tt.envVars {
				require.NoError(t, os.Setenv(k, v))
			}

			result := passthroughEnvVars()
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}
