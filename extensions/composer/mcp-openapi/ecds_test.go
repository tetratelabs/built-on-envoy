// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	_ "embed"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	//go:embed testdata/bridge_ecds.json
	expectedBridgeECDS string

	//go:embed testdata/lua_ecds.json
	expectedLuaECDS string
)

func testRoot(t *testing.T) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestWriteECDSConfig(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, writeECDSConfig(root, "ecds.json", testAPITools(t)))

	actual, err := root.ReadFile("ecds.json")
	require.NoError(t, err)
	require.JSONEq(t, expectedBridgeECDS, string(actual))
}

func TestBuildBridgeTool(t *testing.T) {
	tests := []struct {
		name     string
		tool     tool
		expected bridgeTool
	}{
		{
			"GET",
			tool{Name: "get", httpMethod: "GET", pathTemplate: "/foo"},
			bridgeTool{Name: "get", HTTPRule: bridgeHTTPRule{Get: "/foo"}},
		},
		{
			"POST with body",
			tool{Name: "post", httpMethod: "POST", pathTemplate: "/bar", hasBody: true},
			bridgeTool{Name: "post", HTTPRule: bridgeHTTPRule{Post: "/bar", Body: "*"}},
		},
		{
			"PUT with body",
			tool{Name: "put", httpMethod: "PUT", pathTemplate: "/baz", hasBody: true},
			bridgeTool{Name: "put", HTTPRule: bridgeHTTPRule{Put: "/baz", Body: "*"}},
		},
		{
			"DELETE",
			tool{Name: "del", httpMethod: "DELETE", pathTemplate: "/qux"},
			bridgeTool{Name: "del", HTTPRule: bridgeHTTPRule{Delete: "/qux"}},
		},
		{
			"PATCH with body",
			tool{Name: "patch", httpMethod: "PATCH", pathTemplate: "/quux", hasBody: true},
			bridgeTool{Name: "patch", HTTPRule: bridgeHTTPRule{Patch: "/quux", Body: "*"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, buildBridgeTool(&tt.tool))
		})
	}
}

func TestWriteLuaECDSConfig(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, writeLuaECDSConfig(root, "lua-ecds.json", testAPITools(t)))

	actual, err := root.ReadFile("lua-ecds.json")
	require.NoError(t, err)
	require.JSONEq(t, expectedLuaECDS, string(actual))
}

func testAPITools(t *testing.T) []tool {
	t.Helper()
	doc := loadTestSpec(t, "testdata/test_api.yml")
	tools, err := generateTools(doc, &config{})
	require.NoError(t, err)
	return tools
}
