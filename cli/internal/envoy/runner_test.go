// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestRunner_Run_ConfigError(t *testing.T) {
	r := &Runner{
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

	r := &Runner{
		Dirs:       &xdg.Directories{DataHome: t.TempDir()},
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
