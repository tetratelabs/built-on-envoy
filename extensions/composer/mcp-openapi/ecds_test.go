// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	_ "embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed testdata/bridge_ecds.json
var expectedBridgeECDS string

func TestWriteECDSConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ecds.json")
	require.NoError(t, writeECDSConfig(path, testAPITools(t)))

	actual, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.JSONEq(t, expectedBridgeECDS, string(actual))
}

func TestWriteECDSConfigInvalidSchema(t *testing.T) {
	tools := []tool{{Name: "bad", InputSchema: map[string]any{"type": func() {}}}}
	err := writeECDSConfig(filepath.Join(t.TempDir(), "ecds.json"), tools)
	require.ErrorContains(t, err, "marshaling input schema for tool bad")
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecds.json")

	require.NoError(t, writeFileAtomic(path, []byte("v1")))
	// Keep a handle to the first file: an atomic replace swaps the directory entry
	// instead of rewriting the file, so the old file keeps its content.
	first, err := os.Open(filepath.Clean(path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	require.NoError(t, writeFileAtomic(path, []byte("v2")))

	actual, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.Equal(t, "v2", string(actual))

	old := make([]byte, 2)
	_, err = first.Read(old)
	require.NoError(t, err)
	require.Equal(t, "v1", string(old))

	// No temporary files are left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestWriteFileAtomicErrors(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		err := writeFileAtomic(filepath.Join(t.TempDir(), "missing", "ecds.json"), []byte("v1"))
		require.Error(t, err)
	})
	t.Run("target is a directory", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ecds.json")
		require.NoError(t, os.Mkdir(path, 0o700))
		// Add a file so the directory can't be replaced by the rename.
		require.NoError(t, os.WriteFile(filepath.Join(path, "keep"), nil, 0o600))

		require.Error(t, writeFileAtomic(path, []byte("v1")))

		// The temporary file is removed on failure.
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
	})
}

func TestBuildBridgeTool(t *testing.T) {
	schema := map[string]any{"type": "object"}
	listConfig := bridgeToolListConfig{Description: "desc", InputSchema: `{"type":"object"}`}
	tests := []struct {
		name     string
		tool     tool
		expected bridgeTool
	}{
		{
			"GET",
			tool{Name: "get", Description: "desc", InputSchema: schema, httpMethod: "GET", pathTemplate: "/foo"},
			bridgeTool{Name: "get", HTTPRule: bridgeHTTPRule{Get: "/foo"}, ToolListConfig: listConfig},
		},
		{
			"POST with body",
			tool{Name: "post", Description: "desc", InputSchema: schema, httpMethod: "POST", pathTemplate: "/bar", hasBody: true},
			bridgeTool{Name: "post", HTTPRule: bridgeHTTPRule{Post: "/bar", Body: "*"}, ToolListConfig: listConfig},
		},
		{
			"PUT with body",
			tool{Name: "put", Description: "desc", InputSchema: schema, httpMethod: "PUT", pathTemplate: "/baz", hasBody: true},
			bridgeTool{Name: "put", HTTPRule: bridgeHTTPRule{Put: "/baz", Body: "*"}, ToolListConfig: listConfig},
		},
		{
			"DELETE",
			tool{Name: "del", Description: "desc", InputSchema: schema, httpMethod: "DELETE", pathTemplate: "/qux"},
			bridgeTool{Name: "del", HTTPRule: bridgeHTTPRule{Delete: "/qux"}, ToolListConfig: listConfig},
		},
		{
			"PATCH with body",
			tool{Name: "patch", Description: "desc", InputSchema: schema, httpMethod: "PATCH", pathTemplate: "/quux", hasBody: true},
			bridgeTool{Name: "patch", HTTPRule: bridgeHTTPRule{Patch: "/quux", Body: "*"}, ToolListConfig: listConfig},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := buildBridgeTool(&tt.tool)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func testAPITools(t *testing.T) []tool {
	t.Helper()
	doc := loadTestSpec(t, "testdata/test_api.yml")
	tools, err := generateTools(doc, &config{})
	require.NoError(t, err)
	return tools
}
