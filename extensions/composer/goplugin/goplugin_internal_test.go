// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

import (
	"archive/tar"
	"bytes"
	"debug/buildinfo"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckVersionCompatibility(t *testing.T) {
	// Inject a known dependency into the host dependencies map for testing.
	hostDependencies["github.com/test/dep"] = &debug.Module{
		Path:    "github.com/test/dep",
		Version: "v1.2.3",
		Sum:     "h1:abc123",
	}
	t.Cleanup(func() {
		delete(hostDependencies, "github.com/test/dep")
	})

	tests := []struct {
		name      string
		plugin    *buildinfo.BuildInfo
		buildMode string
		wantErr   string
	}{
		{
			name: "Compatible plugin",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Settings: []debug.BuildSetting{
					{Key: "-buildmode", Value: "plugin"},
				},
				Deps: []*debug.Module{
					{Path: "github.com/test/dep", Version: "v1.2.3", Sum: "h1:abc123"},
				},
			},
			buildMode: "plugin",
		},
		{
			name: "Different Go version",
			plugin: &buildinfo.BuildInfo{
				GoVersion: "go1.0.0",
			},
			buildMode: "plugin",
			wantErr:   "plugin Go version is different from host Go version",
		},
		{
			name: "Wrong buildmode",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Settings: []debug.BuildSetting{
					{Key: "-buildmode", Value: "c-shared"},
				},
			},
			buildMode: "plugin",
			wantErr:   `plugin buildmode is not "plugin"`,
		},
		{
			name: "No buildmode setting",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Settings: []debug.BuildSetting{
					{Key: "-tags", Value: "test"},
				},
			},
			buildMode: "plugin",
		},
		{
			name: "Dependency not found in host",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Deps: []*debug.Module{
					{Path: "github.com/nonexistent/module", Version: "v1.0.0", Sum: "h1:abc"},
				},
			},
			buildMode: "plugin",
			wantErr:   "plugin dependency is not found in host dependencies",
		},
		{
			name: "Dependency version mismatch",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Deps: []*debug.Module{
					{Path: "github.com/test/dep", Version: "v0.0.0-fake", Sum: "h1:abc123"},
				},
			},
			buildMode: "plugin",
			wantErr:   "has different versions",
		},
		{
			name: "Dependency sum mismatch",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Deps: []*debug.Module{
					{Path: "github.com/test/dep", Version: "v1.2.3", Sum: "h1:fake"},
				},
			},
			buildMode: "plugin",
			wantErr:   "has different sums",
		},
		{
			name: "Plugin dependency with Replace directive uses replacement",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Deps: []*debug.Module{
					{
						Path:    "github.com/original/module",
						Version: "v0.0.0",
						Replace: &debug.Module{
							Path:    "github.com/nonexistent/replaced",
							Version: "v1.0.0",
							Sum:     "h1:abc",
						},
					},
				},
			},
			buildMode: "plugin",
			wantErr:   "plugin dependency is not found in host dependencies",
		},
		{
			name: "Plugin dependency with Replace directive matches host",
			plugin: &buildinfo.BuildInfo{
				GoVersion: hostBuildInfo.GoVersion,
				Deps: []*debug.Module{
					{
						Path:    "github.com/original/module",
						Version: "v0.0.0",
						Replace: &debug.Module{
							Path:    "github.com/test/dep",
							Version: "v1.2.3",
							Sum:     "h1:abc123",
						},
					},
				},
			},
			buildMode: "plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkVersionCompatibility(tt.plugin, tt.buildMode)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestCreateFactory(t *testing.T) {
	t.Run("Binary not found", func(t *testing.T) {
		_, err := createFactory[string]("/nonexistent/path.so", "Symbol", "plugin", true)
		require.ErrorContains(t, err, "failed to find a plugin implementation")
	})

	t.Run("Not a Go binary", func(t *testing.T) {
		// Create a temp file that is not a valid Go binary.
		tmpFile := filepath.Join(t.TempDir(), "notgo.so")
		err := os.WriteFile(tmpFile, []byte("not a go binary"), 0o600)
		require.NoError(t, err)

		_, err = createFactory[string](tmpFile, "Symbol", "plugin", true)
		require.ErrorContains(t, err, "failed to read go plugin build info")
	})

	t.Run("Version incompatible with strict check", func(t *testing.T) {
		// Temporarily change the hostBuildInfo GoVersion to force a mismatch.
		origVersion := hostBuildInfo.GoVersion
		hostBuildInfo.GoVersion = "go0.0.0-fake"
		t.Cleanup(func() {
			hostBuildInfo.GoVersion = origVersion
		})

		// Use the test binary itself as the binary path since it's a valid Go binary.
		testBinary, err := os.Executable()
		require.NoError(t, err)

		_, err = createFactory[string](testBinary, "Symbol", "plugin", true)
		require.ErrorContains(t, err, "plugin Go version is different from host Go version")
	})

	// NOTE: Tests for plugin.Open, Lookup, type assertion, and factory map lookup are not
	// included here because plugin.Open on a non-plugin binary causes a fatal runtime panic
	// that cannot be recovered. These paths require a real compiled Go plugin .so file.
}

func TestFetchGoPluginPath_File(t *testing.T) {
	path, err := fetchGoPluginPath("file:///path/to/plugin.so", "name")
	require.NoError(t, err)
	assert.Equal(t, "/path/to/plugin.so", path)
}

func TestFetchGoPluginPath_Unsupported(t *testing.T) {
	_, err := fetchGoPluginPath("http://example.com/plugin", "name")
	require.ErrorContains(t, err, "unsupported")
}

func TestFetchGoPluginPath_OCI(t *testing.T) {
	// Spin up an in-memory registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	require.NoError(t, err)

	// Build a minimal OCI image with a .so file.
	expectedContent := "test-plugin-binary"
	layer, err := newTestLayer(types.OCILayer, map[string][]byte{
		"plugin.so": []byte(expectedContent),
	})
	require.NoError(t, err)
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer})
	require.NoError(t, err)
	img = mutate.MediaType(img, types.OCIManifestSchema1)

	ref := fmt.Sprintf("%s/test/plugin:latest", u.Host)
	err = crane.Push(img, ref)
	require.NoError(t, err)

	cacheDir := t.TempDir()
	t.Setenv("GOPLUGIN_CACHE_DIR", cacheDir)
	t.Setenv("GOPLUGIN_INSECURE", "true")

	pluginPath, err := fetchGoPluginPath("oci://"+ref, "pluginName")
	require.NoError(t, err)

	_, statErr := os.Stat(pluginPath)
	require.NoError(t, statErr, "plugin file does not exist at %s", pluginPath)

	b, err := os.ReadFile(pluginPath) //nolint:gosec // Test code reads from temp dir.
	require.NoError(t, err)
	assert.Equal(t, expectedContent, string(b))

	// Verify it's cached under the expected directory.
	assert.True(t, strings.HasPrefix(pluginPath, filepath.Join(cacheDir, "pluginName")),
		"plugin not cached under expected directory: %s", pluginPath)
}

// newTestLayer creates a mock OCI layer with the given media type and file contents.
func newTestLayer(mediaType types.MediaType, contents map[string][]byte) (v1.Layer, error) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	defer tw.Close() //nolint:errcheck // Test helper cleanup.

	for filename, content := range contents {
		if err := tw.WriteHeader(&tar.Header{
			Name:     filename,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, err
		}
		if _, err := io.CopyN(tw, bytes.NewReader(content), int64(len(content))); err != nil {
			return nil, err
		}
	}
	return partial.UncompressedToLayer(
		&testLayer{
			raw:       b.Bytes(),
			mediaType: mediaType,
		},
	)
}

type testLayer struct {
	raw       []byte
	mediaType types.MediaType
}

func (r *testLayer) DiffID() (v1.Hash, error) { return v1.Hash{}, nil }
func (r *testLayer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewBuffer(r.raw)), nil
}
func (r *testLayer) MediaType() (types.MediaType, error) { return r.mediaType, nil }
