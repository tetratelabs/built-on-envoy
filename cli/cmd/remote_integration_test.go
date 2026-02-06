// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestPushPull(t *testing.T) {
	ctx := t.Context()
	registry, registryAddr, err := internaltesting.StartOCIRegistry(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = registry.Terminate(ctx) })

	// Use the testdata directory which contains a valid extension
	extensionDir := "testdata/push_pull"
	repo := registryAddr + "/tetrateio/built-on-envoy"

	// Create Push command and execute
	push := &Push{
		Local: extensionDir,
		OCI: OCIFlags{
			Registry: repo,
			Insecure: true, // Local registry uses HTTP
		},
	}
	require.NoError(t, push.Validate())      // Validate loads and validates the manifest
	require.NoError(t, push.AfterApply(nil)) // AfterApply creates the OCI client

	// Push the extension
	require.NoError(t, push.Run(ctx))

	// Create a temporary directory to pull the extension to
	pullDir := t.TempDir()

	// Create Pull command and execute
	pull := &Pull{
		Extension: repo + "/extension-src-push-pull:1.0.0",
		Path:      pullDir,
		OCI: OCIFlags{
			Insecure: true,
		},
	}
	require.NoError(t, pull.Validate()) // Validate parses the extension reference

	// Pull the extension
	require.NoError(t, pull.Run(ctx, &xdg.Directories{}))
	require.FileExists(t, pullDir+"/extensions/src-push-pull/1.0.0/manifest.yaml")
	require.FileExists(t, pullDir+"/extensions/src-push-pull/1.0.0/plugin/test.lua")
}
