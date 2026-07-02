// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestBuildWasm(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	extensionPath := "../../../extensions/example-wasm-go"
	fakeDirs := &xdg.Directories{DataHome: t.TempDir()}

	manifest, err := LoadLocalManifest(extensionPath + "/manifest.yaml")
	require.NoError(t, err)
	require.Equal(t, TypeWasm, manifest.Type)

	err = BuildWasm(logger, fakeDirs, manifest, extensionPath)
	require.NoError(t, err)

	// Ensure the compiled module is created with the correct name (plugin.wasm).
	wasmPath := LocalCacheExtension(fakeDirs, manifest)
	_, err = os.Stat(wasmPath)
	require.NoError(t, err, "wasm module should exist at %s", wasmPath)
	require.Contains(t, wasmPath, "plugin.wasm", "wasm module should be named plugin.wasm")

	// The manifest is copied alongside the module so boe can resolve the cached extension.
	_, err = os.Stat(filepath.Join(filepath.Dir(wasmPath), "manifest.yaml"))
	require.NoError(t, err, "manifest.yaml should be copied to the cache directory")
}
