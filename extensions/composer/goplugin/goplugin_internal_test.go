// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestFetchGoPluginPath_File(t *testing.T) {
	path, err := fetchGoPluginPath("file:///path/to/plugin.so", "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/path/to/plugin.so" {
		t.Fatalf("got %q, want %q", path, "/path/to/plugin.so")
	}
}

func TestFetchGoPluginPath_Unsupported(t *testing.T) {
	_, err := fetchGoPluginPath("http://example.com/plugin", "name")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

func TestFetchGoPluginPath_OCI(t *testing.T) {
	// Spin up an in-memory registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Build a minimal OCI image with a .so file.
	expectedContent := "test-plugin-binary"
	layer, err := newTestLayer(types.OCILayer, map[string][]byte{
		"plugin.so": []byte(expectedContent),
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer})
	if err != nil {
		t.Fatal(err)
	}
	img = mutate.MediaType(img, types.OCIManifestSchema1)

	ref := fmt.Sprintf("%s/test/plugin:latest", u.Host)
	if err = crane.Push(img, ref); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	t.Setenv("GOPLUGIN_CACHE_DIR", cacheDir)
	t.Setenv("GOPLUGIN_INSECURE", "true")

	pluginPath, err := fetchGoPluginPath("oci://"+ref, "pluginName")
	if err != nil {
		t.Fatalf("fetchGoPluginPath() error = %v", err)
	}

	if _, statErr := os.Stat(pluginPath); statErr != nil {
		t.Fatalf("plugin file does not exist at %s: %v", pluginPath, statErr)
	}

	b, err := os.ReadFile(pluginPath) //nolint:gosec // Test code reads from temp dir.
	if err != nil {
		t.Fatalf("failed to read plugin: %v", err)
	}
	if string(b) != expectedContent {
		t.Fatalf("content mismatch: got %q want %q", string(b), expectedContent)
	}

	// Verify it's cached under the expected directory.
	if !strings.HasPrefix(pluginPath, filepath.Join(cacheDir, "pluginName")) {
		t.Fatalf("plugin not cached under expected directory: %s", pluginPath)
	}
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
