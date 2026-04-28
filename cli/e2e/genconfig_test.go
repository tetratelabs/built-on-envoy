// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestGenConfigRemoteComposerOCIURL(t *testing.T) {
	internaltesting.SkipIfTestRegistryNotConfigured(t)

	registry := os.Getenv("BOE_REGISTRY")

	// Run genconfig with a remote composer extension.
	// This downloads the extension and composer library from the registry
	// and generates config with oci:// URLs.
	cmd := exec.CommandContext(t.Context(), cliBin, "gen-config", "--extension", "example-go")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "gen-config should succeed: %s", string(output))

	outputStr := string(output)

	// The generated config should contain an oci:// URL for the plugin.
	expectedPrefix := "oci://" + registry + "/extension-example-go:"
	require.Contains(t, outputStr, expectedPrefix,
		"genconfig output should contain oci:// URL with prefix %q", expectedPrefix)

	// The generated config should NOT contain file:// URLs for the plugin.
	require.NotContains(t, outputStr, "file://",
		"genconfig output should not contain file:// URL for remote extension")
}

func TestGenConfigLocalExtensionFileURL(t *testing.T) {
	// Run genconfig with a local Lua extension (no remote registry needed).
	cmd := exec.CommandContext(t.Context(), cliBin, "gen-config",
		"--local", "../../extensions/example-lua",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "gen-config should succeed: %s", string(output))

	outputStr := string(output)

	// Local extensions should not produce oci:// URLs.
	require.NotContains(t, outputStr, "oci://",
		"genconfig output for local extension should not contain oci:// URL")
}
