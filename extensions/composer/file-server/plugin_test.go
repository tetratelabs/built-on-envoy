// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Helper functions

func setupTestFiles(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "emptydir"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("<html>index</html>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte("body {}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README"), []byte("readme content"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data."), []byte("dot-ending"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "page.html"), []byte("<html>page</html>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "index.html"), []byte("<html>subdir index</html>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "empty.txt"), []byte(""), 0o600))
	return tmpDir
}

func testConfig(tmpDir string) *fileServerConfig {
	return &fileServerConfig{
		PathMappings: []pathMapping{
			{RequestPathPrefix: "/static", FilePathPrefix: tmpDir},
			{RequestPathPrefix: "/static/nested", FilePathPrefix: filepath.Join(tmpDir, "subdir")},
		},
		ContentTypes: map[string]string{
			"html": "text/html",
			"css":  "text/css",
			"txt":  "text/plain",
			"":     "text/x-no-suffix",
		},
		DefaultContentType:  "application/octet-stream",
		DirectoryIndexFiles: []string{"index.html", "index.txt"},
	}
}

func createTestFilter(t *testing.T, ctrl *gomock.Controller, tmpDir string) (*fileServerHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	filter := &fileServerHttpFilter{handle: mockHandle, config: testConfig(tmpDir)}
	return filter, mockHandle
}

// newPluginHandleWithoutPerRouteConfig creates a mock HttpFilterHandle with default expectations including
// GetMostSpecificConfig returning nil (no per-route config).
func newPluginHandleWithoutPerRouteConfig(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	pluginHandle.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	return pluginHandle
}

// newPluginHandleWithPerRouteConfig creates a mock HttpFilterHandle that returns
// the given per-route config from GetMostSpecificConfig.
func newPluginHandleWithPerRouteConfig(ctrl *gomock.Controller, perRouteConfig any) *mocks.MockHttpFilterHandle {
	pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	pluginHandle.EXPECT().GetMostSpecificConfig().Return(perRouteConfig).AnyTimes()
	return pluginHandle
}

func findHeaderValue(headers [][2]string, key string) (string, bool) {
	for _, h := range headers {
		if h[0] == key {
			return h[1], true
		}
	}
	return "", false
}

func requireHeaderValue(t *testing.T, headers [][2]string, key, expectedValue string) {
	t.Helper()
	val, ok := findHeaderValue(headers, key)
	require.True(t, ok, "header %q not found", key)
	require.Equal(t, expectedValue, val, "header %q", key)
}

// Tests for config validation

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  fileServerConfig
		wantErr string
	}{
		{
			name: "valid config",
			config: fileServerConfig{
				PathMappings: []pathMapping{{RequestPathPrefix: "/", FilePathPrefix: "/var/www"}},
			},
		},
		{
			name:    "empty path mappings",
			config:  fileServerConfig{},
			wantErr: "at least one path_mapping is required",
		},
		{
			name: "empty request path prefix",
			config: fileServerConfig{
				PathMappings: []pathMapping{{RequestPathPrefix: "", FilePathPrefix: "/var/www"}},
			},
			wantErr: "request_path_prefix must not be empty",
		},
		{
			name: "empty file path prefix",
			config: fileServerConfig{
				PathMappings: []pathMapping{{RequestPathPrefix: "/static", FilePathPrefix: ""}},
			},
			wantErr: "file_path_prefix must not be empty",
		},
		{
			name: "duplicate request path prefix",
			config: fileServerConfig{
				PathMappings: []pathMapping{
					{RequestPathPrefix: "/static", FilePathPrefix: "/a"},
					{RequestPathPrefix: "/static", FilePathPrefix: "/b"},
				},
			},
			wantErr: "duplicate request_path_prefix: /static",
		},
		{
			name: "content type suffix with period",
			config: fileServerConfig{
				PathMappings: []pathMapping{{RequestPathPrefix: "/", FilePathPrefix: "/var/www"}},
				ContentTypes: map[string]string{".html": "text/html"},
			},
			wantErr: "content_types suffix may not contain a period: .html",
		},
		{
			name: "directory index file with slash",
			config: fileServerConfig{
				PathMappings:        []pathMapping{{RequestPathPrefix: "/", FilePathPrefix: "/var/www"}},
				DirectoryIndexFiles: []string{"sub/index.html"},
			},
			wantErr: "directory_index_files entry may not contain a slash: sub/index.html",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

// Tests for applyPathMapping

func TestApplyPathMapping(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		mapping  pathMapping
		wantPath string
		wantOk   bool
	}{
		{
			name:     "simple path",
			path:     "/static/foo.html",
			mapping:  pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantPath: "/var/www/foo.html",
			wantOk:   true,
		},
		{
			name:     "nested path",
			path:     "/static/a/b/c.txt",
			mapping:  pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantPath: "/var/www/a/b/c.txt",
			wantOk:   true,
		},
		{
			name:     "prefix with trailing slash",
			path:     "/static/foo.html",
			mapping:  pathMapping{RequestPathPrefix: "/static/", FilePathPrefix: "/var/www"},
			wantPath: "/var/www/foo.html",
			wantOk:   true,
		},
		{
			name:     "exact prefix match",
			path:     "/static",
			mapping:  pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantPath: "/var/www",
			wantOk:   true,
		},
		{
			name:     "query string stripped",
			path:     "/static/foo.html?v=1",
			mapping:  pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantPath: "/var/www/foo.html",
			wantOk:   true,
		},
		{
			name:    "directory traversal with ..",
			path:    "/static/../etc/passwd",
			mapping: pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantOk:  false,
		},
		{
			name:    "dot component",
			path:    "/static/./foo.html",
			mapping: pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantOk:  false,
		},
		{
			name:    "double slash in path",
			path:    "/static//foo.html",
			mapping: pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantOk:  false,
		},
		{
			name:    "double slash at join point",
			path:    "/static//foo.html",
			mapping: pathMapping{RequestPathPrefix: "/static/", FilePathPrefix: "/var/www"},
			wantOk:  false,
		},
		{
			name:    "multiple leading slashes",
			path:    "/static///foo.html",
			mapping: pathMapping{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			wantOk:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := applyPathMapping(tt.path, &tt.mapping)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Equal(t, tt.wantPath, got)
			}
		})
	}
}

// Tests for findLongestMatchingMapping

func TestFindLongestMatchingMapping(t *testing.T) {
	config := &fileServerConfig{
		PathMappings: []pathMapping{
			{RequestPathPrefix: "/", FilePathPrefix: "/default"},
			{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
			{RequestPathPrefix: "/static/docs", FilePathPrefix: "/var/docs"},
		},
	}

	t.Run("longest match wins", func(t *testing.T) {
		m := config.findLongestMatchingMapping("/static/docs/readme.txt")
		require.NotNil(t, m)
		require.Equal(t, "/var/docs", m.FilePathPrefix)
	})

	t.Run("shorter match when longer does not apply", func(t *testing.T) {
		m := config.findLongestMatchingMapping("/static/index.html")
		require.NotNil(t, m)
		require.Equal(t, "/var/www", m.FilePathPrefix)
	})

	t.Run("root match", func(t *testing.T) {
		m := config.findLongestMatchingMapping("/other")
		require.NotNil(t, m)
		require.Equal(t, "/default", m.FilePathPrefix)
	})

	t.Run("no match", func(t *testing.T) {
		config := &fileServerConfig{
			PathMappings: []pathMapping{{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"}},
		}
		m := config.findLongestMatchingMapping("/other")
		require.Nil(t, m)
	})
}

// Tests for contentTypeForPath

func TestContentTypeForPath(t *testing.T) {
	config := &fileServerConfig{
		ContentTypes: map[string]string{
			"html":   "text/html",
			"css":    "text/css",
			"README": "text/markdown",
			"":       "text/x-no-suffix",
		},
		DefaultContentType: "application/octet-stream",
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"known extension", "/foo/bar.html", "text/html"},
		{"multiple dots uses last", "/foo/bar.baz.css", "text/css"},
		{"unknown extension uses default", "/foo/bar.xyz", "application/octet-stream"},
		{"no extension full filename match", "/foo/README", "text/markdown"},
		{"no extension no match uses default", "/foo/LICENSE", "application/octet-stream"},
		{"empty suffix matches dot-ending files", "/foo/data.", "text/x-no-suffix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, config.contentTypeForPath(tt.path))
		})
	}

	t.Run("empty default content type", func(t *testing.T) {
		c := &fileServerConfig{ContentTypes: map[string]string{"html": "text/html"}}
		require.Empty(t, c.contentTypeForPath("/foo/bar.xyz"))
	})
}

// Tests for parseRangeHeader

func TestParseRangeHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantStart uint64
		wantEnd   uint64
	}{
		{"valid range", "bytes=3-5", 3, 6},
		{"range to end", "bytes=3-", 3, 0},
		{"empty header", "", 0, 0},
		{"invalid prefix", "megatrons=3-5", 0, 0},
		{"multiple ranges", "bytes=1-3,5-7", 0, 0},
		{"suffix range", "bytes=-5", 0, 0},
		{"non-numeric start", "bytes=abc-5", 0, 0},
		{"non-numeric end", "bytes=3-abc", 3, 0},
		{"zero start", "bytes=0-9", 0, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := parseRangeHeader(tt.header)
			require.Equal(t, tt.wantStart, start)
			require.Equal(t, tt.wantEnd, end)
		})
	}
}

// Tests for OnRequestHeaders

func TestOnRequestHeaders_ServeFile(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	var capturedBody []byte
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true).Do(func(body []byte, _ bool) {
		capturedBody = body
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, []byte("<html>index</html>"), capturedBody)
	requireHeaderValue(t, capturedHeaders, ":status", "200")
	requireHeaderValue(t, capturedHeaders, "content-type", "text/html")
	requireHeaderValue(t, capturedHeaders, "content-length", "18")
	requireHeaderValue(t, capturedHeaders, "accept-ranges", "bytes")
}

func TestOnRequestHeaders_HeadRequest(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), true).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"HEAD"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	requireHeaderValue(t, capturedHeaders, ":status", "200")
	requireHeaderValue(t, capturedHeaders, "content-type", "text/html")
	requireHeaderValue(t, capturedHeaders, "content-length", "18")
}

func TestOnRequestHeaders_EmptyPath(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, _ := createTestFilter(t, ctrl, tmpDir)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_NonMatchingPath(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, _ := createTestFilter(t, ctrl, tmpDir)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/other/foo.html"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_MethodNotAllowed(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)
	mockHandle.EXPECT().SendLocalResponse(uint32(405), gomock.Any(), gomock.Any(), "file_server_rejected_method")

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"POST"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestOnRequestHeaders_NonNormalizedPath(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)
	mockHandle.EXPECT().SendLocalResponse(uint32(400), gomock.Any(), gomock.Any(), "file_server_rejected_non_normalized_path")

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/../etc/passwd"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestOnRequestHeaders_FileNotFound(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)
	mockHandle.EXPECT().SendLocalResponse(uint32(404), gomock.Any(), gomock.Any(), "file_server_not_found")

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/nonexistent.html"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestOnRequestHeaders_DirectoryWithIndexFile(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	var capturedBody []byte
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true).Do(func(body []byte, _ bool) {
		capturedBody = body
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/subdir"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, []byte("<html>subdir index</html>"), capturedBody)
	requireHeaderValue(t, capturedHeaders, ":status", "200")
	requireHeaderValue(t, capturedHeaders, "content-type", "text/html")
}

func TestOnRequestHeaders_DirectoryWithoutIndexFile(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)
	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), "file_server_no_valid_directory_index")

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/emptydir"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestOnRequestHeaders_RangeRequest(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	var capturedBody []byte
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true).Do(func(body []byte, _ bool) {
		capturedBody = body
	})

	// index.html content: "<html>index</html>" (18 bytes), requesting bytes 6-10 → "index"
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"GET"},
		"range":   {"bytes=6-10"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, []byte("index"), capturedBody)
	requireHeaderValue(t, capturedHeaders, ":status", "206")
	requireHeaderValue(t, capturedHeaders, "content-length", "5")
	requireHeaderValue(t, capturedHeaders, "content-range", "bytes 6-10/18")
}

func TestOnRequestHeaders_RangeRequestToEnd(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	var capturedBody []byte
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true).Do(func(body []byte, _ bool) {
		capturedBody = body
	})

	// index.html content: "<html>index</html>" (18 bytes), requesting bytes 11- → "</html>"
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"GET"},
		"range":   {"bytes=11-"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, []byte("</html>"), capturedBody)
	requireHeaderValue(t, capturedHeaders, ":status", "206")
	requireHeaderValue(t, capturedHeaders, "content-length", "7")
	requireHeaderValue(t, capturedHeaders, "content-range", "bytes 11-17/18")
}

func TestOnRequestHeaders_RangeNotSatisfiable(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)
	mockHandle.EXPECT().SendLocalResponse(uint32(416), gomock.Any(), gomock.Any(), "file_server_range_not_satisfiable")

	// index.html is 18 bytes, requesting bytes 3-25 is out of range.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"GET"},
		"range":   {"bytes=3-25"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestOnRequestHeaders_RangeStartAtFileSize(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)
	mockHandle.EXPECT().SendLocalResponse(uint32(416), gomock.Any(), gomock.Any(), "file_server_range_not_satisfiable")

	// index.html is 18 bytes. "bytes=18-" means start at byte 18 which is past the last byte.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"GET"},
		"range":   {"bytes=18-"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestOnRequestHeaders_UrlEncodedPath(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedBody []byte
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false)
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true).Do(func(body []byte, _ bool) {
		capturedBody = body
	})

	// %69ndex = "index" after decoding.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/%69ndex.html"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, []byte("<html>index</html>"), capturedBody)
}

func TestOnRequestHeaders_ContentTypeDetection(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/style.css"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	requireHeaderValue(t, capturedHeaders, "content-type", "text/css")
}

func TestOnRequestHeaders_DefaultContentType(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true)

	// README has no extension and no exact filename match in our test config.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/README"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	requireHeaderValue(t, capturedHeaders, "content-type", "application/octet-stream")
}

func TestOnRequestHeaders_EmptyFile(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	// Empty file: SendResponseHeaders with endOfStream=true, no SendResponseData.
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), true).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/empty.txt"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	requireHeaderValue(t, capturedHeaders, ":status", "200")
	requireHeaderValue(t, capturedHeaders, "content-length", "0")
	requireHeaderValue(t, capturedHeaders, "content-type", "text/plain")
}

func TestOnRequestHeaders_LongestPrefixMatchUsed(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedBody []byte
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false)
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true).Do(func(body []byte, _ bool) {
		capturedBody = body
	})

	// /static/nested maps to subdir, so /static/nested/page.html → subdir/page.html.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/nested/page.html"},
		":method": {"GET"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, []byte("<html>page</html>"), capturedBody)
}

func TestOnRequestHeaders_InvalidRangeFormatIgnored(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), false).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})
	mockHandle.EXPECT().SendResponseData(gomock.Any(), true)

	// Invalid range format is ignored and full file is served.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"GET"},
		"range":   {"megatrons=3-5"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	requireHeaderValue(t, capturedHeaders, ":status", "200")
	requireHeaderValue(t, capturedHeaders, "content-length", "18")
}

func TestOnRequestHeaders_HeadWithRange(t *testing.T) {
	tmpDir := setupTestFiles(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter, mockHandle := createTestFilter(t, ctrl, tmpDir)

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendResponseHeaders(gomock.Any(), true).Do(func(headers [][2]string, _ bool) {
		capturedHeaders = headers
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":   {"/static/index.html"},
		":method": {"HEAD"},
		"range":   {"bytes=0-4"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	requireHeaderValue(t, capturedHeaders, ":status", "206")
	requireHeaderValue(t, capturedHeaders, "content-length", "5")
	requireHeaderValue(t, capturedHeaders, "content-range", "bytes 0-4/18")
}

// Tests for FileServerHttpFilterConfigFactory

func TestFileServerConfigFactory_ValidConfig(t *testing.T) {
	config := fileServerConfig{
		PathMappings: []pathMapping{
			{RequestPathPrefix: "/static", FilePathPrefix: "/var/www"},
		},
		ContentTypes:       map[string]string{"html": "text/html"},
		DefaultContentType: "application/octet-stream",
	}
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &FileServerHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockHandle, configJSON)

	require.NoError(t, err)
	require.NotNil(t, filterFactory)
	fsFactory, ok := filterFactory.(*fileServerHttpFilterFactory)
	require.True(t, ok)
	require.Len(t, fsFactory.config.PathMappings, 1)
	require.Equal(t, "/static", fsFactory.config.PathMappings[0].RequestPathPrefix)
}

func TestFileServerConfigFactory_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &FileServerHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockHandle, []byte{})

	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestFileServerConfigFactory_NilConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &FileServerHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockHandle, nil)

	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestFileServerConfigFactory_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any())

	factory := &FileServerHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockHandle, []byte("{invalid"))

	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestFileServerConfigFactory_InvalidConfig(t *testing.T) {
	configJSON := []byte(`{"path_mappings": []}`)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any())

	factory := &FileServerHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockHandle, configJSON)

	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "at least one path_mapping is required")
}

// Tests for filter factory

func TestFileServerFilterFactory_Create(t *testing.T) {
	config := &fileServerConfig{
		PathMappings: []pathMapping{{RequestPathPrefix: "/", FilePathPrefix: "/var/www"}},
	}
	factory := &fileServerHttpFilterFactory{config: config}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newPluginHandleWithoutPerRouteConfig(ctrl)

	filter := factory.Create(mockHandle)
	require.NotNil(t, filter)
	fsFilter, ok := filter.(*fileServerHttpFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, fsFilter.handle)
	require.Equal(t, config, fsFilter.config)
}

func Test_CreatePerRoute(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid config", func(t *testing.T) {
		cfg := map[string]any{
			"path_mappings": []map[string]any{
				{"request_path_prefix": "/static", "file_path_prefix": tmpDir},
			},
		}
		cfgJSON, err := json.Marshal(cfg)
		require.NoError(t, err)

		result, err := (&FileServerHttpFilterConfigFactory{}).CreatePerRoute(cfgJSON)
		require.NoError(t, err)
		require.NotNil(t, result)
		perRoute, ok := result.(*fileServerConfig)
		require.True(t, ok)
		assert.Len(t, perRoute.PathMappings, 1)
		assert.Equal(t, "/static", perRoute.PathMappings[0].RequestPathPrefix)
	})

	t.Run("empty config returns empty config", func(t *testing.T) {
		result, err := (&FileServerHttpFilterConfigFactory{}).CreatePerRoute([]byte{})
		require.NoError(t, err)
		perRoute, ok := result.(*fileServerConfig)
		require.True(t, ok)
		assert.Empty(t, perRoute.PathMappings)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		result, err := (&FileServerHttpFilterConfigFactory{}).CreatePerRoute([]byte(`{invalid`))
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("invalid config returns error", func(t *testing.T) {
		cfgJSON := []byte(`{"path_mappings":[{"request_path_prefix":"","file_path_prefix":"/tmp"}]}`)
		result, err := (&FileServerHttpFilterConfigFactory{}).CreatePerRoute(cfgJSON)
		require.Error(t, err)
		require.Nil(t, result)
	})
}

func Test_PerRouteConfigOverride(t *testing.T) {
	tmpDir := t.TempDir()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseConfig := &fileServerConfig{
		PathMappings: []pathMapping{{RequestPathPrefix: "/base", FilePathPrefix: "/base/path"}},
	}
	baseFactory := &fileServerHttpFilterFactory{config: baseConfig}

	t.Run("per-route config overrides factory config", func(t *testing.T) {
		perRouteConfig := &fileServerConfig{
			PathMappings: []pathMapping{{RequestPathPrefix: "/override", FilePathPrefix: tmpDir}},
		}
		perRoute := perRouteConfig
		mockHandle := newPluginHandleWithPerRouteConfig(ctrl, perRoute)
		filter := baseFactory.Create(mockHandle)
		fsFilter, ok := filter.(*fileServerHttpFilter)
		require.True(t, ok)
		assert.Equal(t, perRouteConfig, fsFilter.config)
	})

	t.Run("nil per-route config uses factory config", func(t *testing.T) {
		mockHandle := newPluginHandleWithoutPerRouteConfig(ctrl)
		filter := baseFactory.Create(mockHandle)
		fsFilter, ok := filter.(*fileServerHttpFilter)
		require.True(t, ok)
		assert.Equal(t, baseConfig, fsFilter.config)
	})

	t.Run("non-matching per-route config type uses factory config", func(t *testing.T) {
		mockHandle := newPluginHandleWithPerRouteConfig(ctrl, "not-a-per-route-config")
		filter := baseFactory.Create(mockHandle)
		fsFilter, ok := filter.(*fileServerHttpFilter)
		require.True(t, ok)
		assert.Equal(t, baseConfig, fsFilter.config)
	})
}

// Tests for WellKnownHttpFilterConfigFactories

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()

	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "file-server")

	factory, ok := factories["file-server"].(*FileServerHttpFilterConfigFactory)
	require.True(t, ok)
	require.NotNil(t, factory)
}
