// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

func TestCheckOrBuildExtProc_Unsupported(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}
	tempDir := t.TempDir()

	manifest := &Manifest{
		Name:    "test-extension",
		Version: "1.0.0",
		Type:    TypeExtProc,
	}

	err := CheckOrBuildExtProcBinary(logger, fakeDirs, manifest, tempDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported ext_proc extension: no go.mod found")
}

func TestCheckOrBuildExtProcBinary(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	extensionPath := "../../../extensions/example-ext-proc"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)
	require.Equal(t, TypeExtProc, manifest.Type)

	err = CheckOrBuildExtProcBinary(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// Ensure the extproc binary is created with the correct name
	libPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(libPath)
	require.NoError(t, err, "extproc binary should exist at %s", libPath)

	require.Contains(t, libPath, "ext_proc-server",
		"extproc binary should be named ext_proc-server (original manifest name)")

	// Run again to verify it uses the cached binary and doesn't fail
	err = CheckOrBuildExtProcBinary(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err, "should not fail when extproc binary is already cached")
}
