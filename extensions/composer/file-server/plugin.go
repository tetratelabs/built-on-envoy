// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the file-server extension.
package impl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// pathMapping maps a request URL prefix to a filesystem path prefix.
type pathMapping struct {
	// RequestPathPrefix is the URL path prefix to match against incoming requests.
	RequestPathPrefix string `json:"request_path_prefix"`
	// FilePathPrefix is the filesystem path prefix that replaces the matched request path prefix.
	FilePathPrefix string `json:"file_path_prefix"`
}

// Config represents the JSON configuration for this filter.
type fileServerConfig struct {
	// PathMappings maps request URL prefixes to filesystem path prefixes.
	// The longest matching prefix takes precedence.
	PathMappings []pathMapping `json:"path_mappings"`
	// ContentTypes maps filename suffixes to content-type header values (e.g., {"html": "text/html"}).
	// Suffixes should not contain a period. Files with no extension can match by full filename.
	ContentTypes map[string]string `json:"content_types"`
	// DefaultContentType is used when no suffix match is found in ContentTypes.
	// If empty, the content-type header is omitted.
	DefaultContentType string `json:"default_content_type"`
	// DirectoryIndexFiles lists filenames to try when the requested path resolves to a directory.
	// Files are tried in order until one is found. Defaults to empty (no directory index).
	DirectoryIndexFiles []string `json:"directory_index_files"`
}

func (c *fileServerConfig) validate() error {
	if len(c.PathMappings) == 0 {
		return fmt.Errorf("at least one path_mapping is required")
	}
	seen := make(map[string]bool)
	for _, m := range c.PathMappings {
		if m.RequestPathPrefix == "" {
			return fmt.Errorf("request_path_prefix must not be empty")
		}
		if m.FilePathPrefix == "" {
			return fmt.Errorf("file_path_prefix must not be empty")
		}
		if seen[m.RequestPathPrefix] {
			return fmt.Errorf("duplicate request_path_prefix: %s", m.RequestPathPrefix)
		}
		seen[m.RequestPathPrefix] = true
	}
	for suffix := range c.ContentTypes {
		if strings.Contains(suffix, ".") {
			return fmt.Errorf("content_types suffix may not contain a period: %s", suffix)
		}
	}
	for _, f := range c.DirectoryIndexFiles {
		if strings.Contains(f, "/") {
			return fmt.Errorf("directory_index_files entry may not contain a slash: %s", f)
		}
	}
	return nil
}

// findLongestMatchingMapping returns the longest matching path mapping for the given path.
func (c *fileServerConfig) findLongestMatchingMapping(requestPath string) *pathMapping {
	var best *pathMapping
	for i := range c.PathMappings {
		m := &c.PathMappings[i]
		if strings.HasPrefix(requestPath, m.RequestPathPrefix) {
			if best == nil || len(m.RequestPathPrefix) > len(best.RequestPathPrefix) {
				best = m
			}
		}
	}
	return best
}

// applyPathMapping maps a request path to a filesystem path using the given mapping.
// Returns the filesystem path and true if valid, or empty string and false if the
// path is rejected (e.g., directory traversal or non-normalized components).
func applyPathMapping(requestPath string, mapping *pathMapping) (string, bool) {
	pathOnly := requestPath
	if idx := strings.IndexByte(pathOnly, '?'); idx != -1 {
		pathOnly = pathOnly[:idx]
	}
	remaining := strings.TrimPrefix(pathOnly, mapping.RequestPathPrefix)

	// Handle the leading slash at the join point.
	if strings.HasPrefix(remaining, "/") {
		if strings.HasSuffix(mapping.RequestPathPrefix, "/") {
			return "", false
		}
		remaining = remaining[1:]
	}
	if strings.HasPrefix(remaining, "/") {
		return "", false
	}

	// Reject path components that could escape the prefix.
	if remaining != "" {
		for _, part := range strings.Split(remaining, "/") {
			if part == ".." || part == "." || part == "" {
				return "", false
			}
		}
	}

	return filepath.Join(mapping.FilePathPrefix, remaining), true
}

// contentTypeForPath returns the content-type for the given file path based on configuration.
func (c *fileServerConfig) contentTypeForPath(filePath string) string {
	ext := filepath.Ext(filePath)
	if ext == "" {
		if ct, ok := c.ContentTypes[filepath.Base(filePath)]; ok {
			return ct
		}
	} else {
		if ct, ok := c.ContentTypes[ext[1:]]; ok {
			return ct
		}
	}
	return c.DefaultContentType
}

// parseRangeHeader parses a Range header value and returns start, end byte positions.
// Returns 0, 0 if the header is absent, malformed, or uses unsupported features.
// The returned end value is exclusive (one past the last byte).
func parseRangeHeader(rangeHeader string) (uint64, uint64) {
	if rangeHeader == "" || !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0
	}
	rangeSpec := rangeHeader[len("bytes="):]
	if strings.Contains(rangeSpec, ",") {
		return 0, 0
	}
	parts := strings.SplitN(rangeSpec, "-", 2)
	if len(parts) != 2 || parts[0] == "" {
		return 0, 0
	}
	start, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0
	}
	if parts[1] == "" {
		return start, 0
	}
	end, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return start, 0
	}
	// Convert inclusive end to exclusive.
	return start, end + 1
}

// readFileRange reads a range of bytes from a file.
func readFileRange(filePath string, offset, length int64) ([]byte, error) {
	f, err := os.Open(filePath) //nolint:gosec // Path is validated by applyPathMapping before reaching here.
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

// This is the implementation of the HTTP filter.
type fileServerHttpFilter struct { //nolint:revive
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *fileServerConfig
}

func (f *fileServerHttpFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	requestPath := headers.GetOne(":path").ToUnsafeString()
	if requestPath == "" {
		return shared.HeadersStatusContinue
	}

	decodedPath, err := url.PathUnescape(requestPath)
	if err != nil {
		return shared.HeadersStatusContinue
	}

	mapping := f.config.findLongestMatchingMapping(decodedPath)
	if mapping == nil {
		return shared.HeadersStatusContinue
	}

	method := headers.GetOne(":method").ToUnsafeString()
	if method != "GET" && method != "HEAD" {
		f.handle.SendLocalResponse(405, [][2]string{{"content-type", "text/plain"}},
			[]byte("Method Not Allowed"), "file_server_rejected_method")
		return shared.HeadersStatusStopAllAndBuffer
	}

	filePath, ok := applyPathMapping(decodedPath, mapping)
	if !ok {
		f.handle.SendLocalResponse(400, [][2]string{{"content-type", "text/plain"}},
			[]byte("Bad Request"), "file_server_rejected_non_normalized_path")
		return shared.HeadersStatusStopAllAndBuffer
	}

	info, err := os.Stat(filePath)
	if err != nil {
		switch {
		case os.IsNotExist(err):
			f.handle.SendLocalResponse(404, [][2]string{{"content-type", "text/plain"}},
				[]byte("Not Found"), "file_server_not_found")
		case os.IsPermission(err):
			f.handle.SendLocalResponse(403, [][2]string{{"content-type", "text/plain"}},
				[]byte("Forbidden"), "file_server_permission_denied")
		default:
			f.handle.Log(shared.LogLevelError, "file-server: stat error: "+err.Error())
			f.handle.SendLocalResponse(500, [][2]string{{"content-type", "text/plain"}},
				[]byte("Internal Server Error"), "file_server_stat_error")
		}
		return shared.HeadersStatusStopAllAndBuffer
	}

	// Handle directory requests by trying index files.
	if info.IsDir() {
		filePath, info, err = f.resolveDirectoryIndex(filePath)
		if err != nil {
			f.handle.SendLocalResponse(403, [][2]string{{"content-type", "text/plain"}},
				[]byte("Forbidden"), "file_server_no_valid_directory_index")
			return shared.HeadersStatusStopAllAndBuffer
		}
	}

	fileSize := uint64(info.Size()) //nolint:gosec // File size is always non-negative.
	start, end := parseRangeHeader(headers.GetOne("range").ToUnsafeString())
	if start > fileSize || (end != 0 && end > fileSize) || (end != 0 && end < start) {
		f.handle.SendLocalResponse(416, [][2]string{
			{"content-type", "text/plain"},
			{"accept-ranges", "bytes"},
		}, []byte("Range Not Satisfiable"), "file_server_range_not_satisfiable")
		return shared.HeadersStatusStopAllAndBuffer
	}

	contentType := f.config.contentTypeForPath(filePath)
	responseHeaders := [][2]string{
		{"accept-ranges", "bytes"},
	}
	if contentType != "" {
		responseHeaders = append(responseHeaders, [2]string{"content-type", contentType})
	}

	var status string
	var body []byte

	if start != 0 || end != 0 {
		if end == 0 {
			end = fileSize
		}
		if start >= end {
			f.handle.SendLocalResponse(416, [][2]string{
				{"content-type", "text/plain"},
				{"accept-ranges", "bytes"},
			}, []byte("Range Not Satisfiable"), "file_server_range_not_satisfiable")
			return shared.HeadersStatusStopAllAndBuffer
		}
		contentLength := end - start
		status = "206"
		responseHeaders = append(responseHeaders,
			[2]string{"content-length", strconv.FormatUint(contentLength, 10)},
			[2]string{"content-range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, fileSize)},
		)
		if method != "HEAD" {
			body, err = readFileRange(filePath, int64(start), int64(contentLength)) //nolint:gosec // Range is validated against file size.
			if err != nil {
				f.handle.Log(shared.LogLevelError, "file-server: read error: "+err.Error())
				f.handle.SendLocalResponse(500, [][2]string{{"content-type", "text/plain"}},
					[]byte("Internal Server Error"), "file_server_read_error")
				return shared.HeadersStatusStopAllAndBuffer
			}
		}
	} else {
		status = "200"
		responseHeaders = append(responseHeaders,
			[2]string{"content-length", strconv.FormatUint(fileSize, 10)},
		)
		if method != "HEAD" {
			body, err = os.ReadFile(filePath) //nolint:gosec // Path is validated by applyPathMapping before reaching here.
			if err != nil {
				f.handle.Log(shared.LogLevelError, "file-server: read error: "+err.Error())
				f.handle.SendLocalResponse(500, [][2]string{{"content-type", "text/plain"}},
					[]byte("Internal Server Error"), "file_server_read_error")
				return shared.HeadersStatusStopAllAndBuffer
			}
		}
	}

	// Prepend :status to the response headers.
	responseHeaders = append([][2]string{{":status", status}}, responseHeaders...)

	if method == "HEAD" || len(body) == 0 {
		f.handle.SendResponseHeaders(responseHeaders, true)
	} else {
		f.handle.SendResponseHeaders(responseHeaders, false)
		f.handle.SendResponseData(body, true)
	}
	return shared.HeadersStatusStopAllAndBuffer
}

// resolveDirectoryIndex tries each configured index file in order and returns
// the path and info of the first one found.
func (f *fileServerHttpFilter) resolveDirectoryIndex(dirPath string) (string, os.FileInfo, error) {
	for _, indexFile := range f.config.DirectoryIndexFiles {
		indexPath := filepath.Join(dirPath, indexFile)
		info, err := os.Stat(indexPath)
		if err == nil && !info.IsDir() {
			return indexPath, info, nil
		}
	}
	return "", nil, os.ErrNotExist
}

// This is the factory for the HTTP filter.
type fileServerHttpFilterFactory struct { //nolint:revive
	shared.EmptyHttpFilterFactory
	config *fileServerConfig
}

func (f *fileServerHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	config := f.config

	// Check for per-route config and override if present.
	if perRoute := pkg.GetMostSpecificConfig[*fileServerConfig](handle); perRoute != nil {
		config = perRoute
	}

	return &fileServerHttpFilter{handle: handle, config: config}
}

// FileServerHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type FileServerHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

func parseConfig(config []byte) (*fileServerConfig, error) {
	cfg := &fileServerConfig{}
	if len(config) > 0 {
		if err := json.Unmarshal(config, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
		if err := cfg.validate(); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	return cfg, nil
}

// Create parses the JSON configuration and creates a factory for the HTTP filter.
// When config is nil or empty the filter starts as a no-op pass-through.
func (f *FileServerHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelWarn, "file-server: no config provided, filter will pass through all requests")
	}
	cfg, err := parseConfig(config)
	if err != nil {
		handle.Log(shared.LogLevelError, "file-server: %s", err.Error())
		return nil, err
	}
	handle.Log(shared.LogLevelInfo, fmt.Sprintf("file-server: loaded config with %d path mapping(s)", len(cfg.PathMappings)))
	return &fileServerHttpFilterFactory{config: cfg}, nil
}

// CreatePerRoute parses the per-route configuration.
func (f *FileServerHttpFilterConfigFactory) CreatePerRoute(unparsedConfig []byte) (any, error) {
	return parseConfig(unparsedConfig)
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		"file-server": &FileServerHttpFilterConfigFactory{},
	}
}
