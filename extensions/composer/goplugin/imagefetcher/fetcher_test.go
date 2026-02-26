// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package imagefetcher

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestExtractImageLayer(t *testing.T) {
	tests := []struct {
		name      string
		mediaType types.MediaType
	}{
		{"Docker", types.DockerLayer},
		{"OCI", types.OCILayer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("no layers", func(t *testing.T) {
				dest := filepath.Join(t.TempDir(), "plugin.so")
				err := extractImageLayer(empty.Image, dest, tt.mediaType)
				if err == nil || err.Error() != "number of layers must be greater than zero" {
					t.Fatal("extractImageLayer should fail due to empty image")
				}
			})

			t.Run("valid layers", func(t *testing.T) {
				previousLayer, err := newMockLayer(types.DockerLayer, nil)
				if err != nil {
					t.Fatal(err)
				}

				exp := "this is plugin binary"
				lastLayer, err := newMockLayer(tt.mediaType, map[string][]byte{
					"plugin.so": []byte(exp),
				})
				if err != nil {
					t.Fatal(err)
				}

				tCases := map[string]int{
					"one layer":           0,
					"more than one layer": 1,
				}

				for name, numberOfPreviousLayers := range tCases {
					t.Run(name, func(t *testing.T) {
						img := empty.Image
						for i := 0; i < numberOfPreviousLayers; i++ {
							img, err = mutate.Append(img, mutate.Addendum{Layer: previousLayer})
							if err != nil {
								t.Fatal(err)
							}
						}

						img, err = mutate.Append(img, mutate.Addendum{Layer: lastLayer})
						if err != nil {
							t.Fatal(err)
						}
						dest := filepath.Join(t.TempDir(), "plugin.so")
						if err := extractImageLayer(img, dest, tt.mediaType); err != nil {
							t.Fatalf("extractImageLayer failed: %v", err)
						}
						b, err := os.ReadFile(dest) //nolint:gosec // Test code reads from temp dir.
						if err != nil {
							t.Fatalf("failed to read written plugin: %v", err)
						}
						if string(b) != exp {
							t.Fatalf("got %s, but want %s", string(b), exp)
						}
					})
				}
			})

			t.Run("invalid media type", func(t *testing.T) {
				l, err := newMockLayer(types.DockerPluginConfig, nil)
				if err != nil {
					t.Fatal(err)
				}
				img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
				if err != nil {
					t.Fatal(err)
				}
				dest := filepath.Join(t.TempDir(), "plugin.so")
				err = extractImageLayer(img, dest, tt.mediaType)
				if err == nil || !strings.Contains(err.Error(), "invalid media type") {
					t.Fatal("extractImageLayer should fail due to invalid media type")
				}
			})
		})
	}
}

func TestExtractPluginBinary(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)

		exp := "hello"
		if err := tw.WriteHeader(&tar.Header{
			Name: "plugin.so",
			Size: int64(len(exp)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, exp); err != nil {
			t.Fatal(err)
		}
		_ = tw.Close()
		_ = gz.Close()

		dest := filepath.Join(t.TempDir(), "plugin.so")
		if err := extractPluginBinary(buf, dest); err != nil {
			t.Errorf("extractPluginBinary failed: %v", err)
		}
		b, err := os.ReadFile(dest) //nolint:gosec // Test code reads from temp dir.
		if err != nil {
			t.Fatalf("failed to read written plugin: %v", err)
		}
		if string(b) != exp {
			t.Errorf("extractPluginBinary got %v, but want %v", string(b), exp)
		}
	})

	t.Run("ok with relative path prefix", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)

		exp := "hello"
		if err := tw.WriteHeader(&tar.Header{
			Name: "./plugin.so",
			Size: int64(len(exp)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, exp); err != nil {
			t.Fatal(err)
		}
		_ = tw.Close()
		_ = gz.Close()

		dest := filepath.Join(t.TempDir(), "plugin.so")
		if err := extractPluginBinary(buf, dest); err != nil {
			t.Errorf("extractPluginBinary failed: %v", err)
		}
		b, err := os.ReadFile(dest) //nolint:gosec // Test code reads from temp dir.
		if err != nil {
			t.Fatalf("failed to read written plugin: %v", err)
		}
		if string(b) != exp {
			t.Errorf("extractPluginBinary got %v, but want %v", string(b), exp)
		}
	})

	t.Run("not found", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{
			Name: "non-plugin.txt",
			Size: int64(1),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		_ = tw.Close()
		_ = gz.Close()
		dest := filepath.Join(t.TempDir(), "plugin.so")
		err := extractPluginBinary(buf, dest)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("extractPluginBinary must fail with not found")
		}
	})

	t.Run("oversized", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{
			Name: "plugin.so",
			Size: maxPluginSize + 1,
		}); err != nil {
			t.Fatal(err)
		}
		_ = tw.Close()
		_ = gz.Close()
		dest := filepath.Join(t.TempDir(), "plugin.so")
		err := extractPluginBinary(buf, dest)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("extractPluginBinary must fail with too large, got: %v", err)
		}
	})
}

func TestPluginKeyChain(t *testing.T) {
	dockerjson := fmt.Sprintf(`{"auths": {"test.io": {"auth": %q}}}`, encode("foo", "bar"))
	keyChain := pluginKeyChain{data: []byte(dockerjson)}
	testRegistry, _ := name.NewRegistry("test.io", name.WeakValidation)
	auth, err := keyChain.Resolve(testRegistry)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	got, err := auth.Authorization()
	if err != nil {
		t.Fatal(err)
	}
	want := &authn.AuthConfig{
		Username: "foo",
		Password: "bar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPluginKeyChain_Anonymous(t *testing.T) {
	dockerjson := fmt.Sprintf(`{"auths": {"other.io": {"auth": %q}}}`, encode("foo", "bar"))
	keyChain := pluginKeyChain{data: []byte(dockerjson)}
	testRegistry, _ := name.NewRegistry("unknown.io", name.WeakValidation)
	auth, err := keyChain.Resolve(testRegistry)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if auth != authn.Anonymous {
		t.Errorf("expected Anonymous authenticator for unknown registry")
	}
}

func TestPluginKeyChain_NullData(t *testing.T) {
	keyChain := pluginKeyChain{data: []byte("null")}
	testRegistry, _ := name.NewRegistry("test.io", name.WeakValidation)
	_, err := keyChain.Resolve(testRegistry)
	if err == nil {
		t.Fatal("expected error for null credential data")
	}
}

func encode(user, pass string) string {
	delimited := fmt.Sprintf("%s:%s", user, pass)
	return base64.StdEncoding.EncodeToString([]byte(delimited))
}

func TestFetchPlugin(t *testing.T) {
	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Build an OCI-standard image with a single gzip tar layer containing plugin.so.
	expectedContent := "hello from plugin"
	layer, err := newMockLayer(types.OCILayer, map[string][]byte{
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

	// Push the image to the registry.
	ref := fmt.Sprintf("%s/test/fetchplugin:latest", u.Host)
	err = crane.Push(img, ref)
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	opt := Option{CacheDir: tmpDir}

	pluginName := "myplugin"
	filePath, err := FetchPlugin(context.Background(), ref, pluginName, opt)
	if err != nil {
		t.Fatalf("FetchPlugin() error = %v", err)
	}
	if filePath == "" {
		t.Fatal("FetchPlugin() returned empty path")
	}

	// Validate file content.
	b, err := os.ReadFile(filePath) //nolint:gosec // Test code reads from temp dir.
	if err != nil {
		t.Fatalf("failed reading plugin file %s: %v", filePath, err)
	}
	if string(b) != expectedContent {
		t.Fatalf("plugin file content mismatch: got %q want %q", string(b), expectedContent)
	}

	// Validate cache reuse: second fetch should return the same path without error.
	filePath2, err := FetchPlugin(context.Background(), ref, pluginName, opt)
	if err != nil {
		t.Fatalf("FetchPlugin() second call error = %v", err)
	}
	if filePath2 != filePath {
		t.Fatalf("expected same cached path, got %s vs %s", filePath2, filePath)
	}

	// Validate path structure: {cacheDir}/{pluginName}/{digest}.so
	d, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	expectedPath := filepath.Join(tmpDir, pluginName, d.Hex+".so")
	if filePath != expectedPath {
		t.Fatalf("path mismatch: got %s want %s", filePath, expectedPath)
	}
}

func TestOptionFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		envs     map[string]string
		wantOpt  Option
		wantData []byte // expected PullSecret content (nil means no secret)
	}{
		{
			name: "defaults",
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
			},
		},
		{
			name: "custom cache dir",
			envs: map[string]string{"GOPLUGIN_CACHE_DIR": "/custom/cache"},
			wantOpt: Option{
				CacheDir: "/custom/cache",
			},
		},
		{
			name: "cache dir from BOE_DATA_HOME",
			envs: map[string]string{"BOE_DATA_HOME": "/boe/data"},
			wantOpt: Option{
				CacheDir: filepath.Join("/boe/data", "goplugin-cache"),
			},
		},
		{
			name: "GOPLUGIN_CACHE_DIR takes precedence over BOE_DATA_HOME",
			envs: map[string]string{
				"GOPLUGIN_CACHE_DIR": "/custom/cache",
				"BOE_DATA_HOME":      "/boe/data",
			},
			wantOpt: Option{
				CacheDir: "/custom/cache",
			},
		},
		{
			name: "insecure true",
			envs: map[string]string{"GOPLUGIN_INSECURE": "true"},
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
				Insecure: true,
			},
		},
		{
			name: "insecure from BOE_REGISTRY_INSECURE",
			envs: map[string]string{"BOE_REGISTRY_INSECURE": "true"},
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
				Insecure: true,
			},
		},
		{
			name: "GOPLUGIN_INSECURE takes precedence over BOE_REGISTRY_INSECURE",
			envs: map[string]string{
				"GOPLUGIN_INSECURE":     "false",
				"BOE_REGISTRY_INSECURE": "true",
			},
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
				Insecure: false,
			},
		},
		{
			name: "insecure false value",
			envs: map[string]string{"GOPLUGIN_INSECURE": "false"},
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
				Insecure: false,
			},
		},
		{
			name: "pull secret from file",
			envs: map[string]string{"GOPLUGIN_PULL_SECRET": "PLACEHOLDER"},
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
			},
			wantData: []byte(`{"auths":{}}`),
		},
		{
			name: "pull secret missing file",
			envs: map[string]string{"GOPLUGIN_PULL_SECRET": "/nonexistent/path"},
			wantOpt: Option{
				CacheDir: filepath.Join(os.TempDir(), "goplugin-cache"),
			},
		},
		{
			name: "all options",
			envs: map[string]string{
				"GOPLUGIN_CACHE_DIR":   "/my/cache",
				"GOPLUGIN_INSECURE":    "true",
				"GOPLUGIN_PULL_SECRET": "PLACEHOLDER",
			},
			wantOpt: Option{
				CacheDir: "/my/cache",
				Insecure: true,
			},
			wantData: []byte(`{"auths":{}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant env vars first.
			for _, key := range []string{"GOPLUGIN_CACHE_DIR", "GOPLUGIN_PULL_SECRET", "GOPLUGIN_INSECURE", "BOE_DATA_HOME", "BOE_REGISTRY_INSECURE"} {
				t.Setenv(key, "")
				os.Unsetenv(key) //nolint:errcheck // best-effort cleanup
			}

			// If a test case needs a real pull secret file, create one.
			for k, v := range tt.envs {
				if k == "GOPLUGIN_PULL_SECRET" && v == "PLACEHOLDER" && tt.wantData != nil {
					f := filepath.Join(t.TempDir(), "dockerconfig.json")
					if err := os.WriteFile(f, tt.wantData, 0o600); err != nil {
						t.Fatal(err)
					}
					v = f
				}
				t.Setenv(k, v)
			}

			got := OptionFromEnv()

			if got.CacheDir != tt.wantOpt.CacheDir {
				t.Errorf("CacheDir = %q, want %q", got.CacheDir, tt.wantOpt.CacheDir)
			}
			if got.Insecure != tt.wantOpt.Insecure {
				t.Errorf("Insecure = %v, want %v", got.Insecure, tt.wantOpt.Insecure)
			}
			if tt.wantData != nil {
				if !bytes.Equal(got.PullSecret, tt.wantData) {
					t.Errorf("PullSecret = %q, want %q", got.PullSecret, tt.wantData)
				}
			} else if got.PullSecret != nil {
				t.Errorf("PullSecret = %q, want nil", got.PullSecret)
			}
		})
	}
}

func TestFetchPlugin_DockerFormat(t *testing.T) {
	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Build a Docker-format image with a single layer.
	expectedContent := "docker plugin binary"
	layer, err := newMockLayer(types.DockerLayer, map[string][]byte{
		"plugin.so": []byte(expectedContent),
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer})
	if err != nil {
		t.Fatal(err)
	}
	img = mutate.MediaType(img, types.DockerManifestSchema2)

	// Push the image to the registry.
	ref := fmt.Sprintf("%s/test/dockerplugin:latest", u.Host)
	if err = crane.Push(img, ref); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	opt := Option{CacheDir: tmpDir}

	filePath, err := FetchPlugin(context.Background(), ref, "dockerplugin", opt)
	if err != nil {
		t.Fatalf("FetchPlugin() error = %v", err)
	}
	if filePath == "" {
		t.Fatal("FetchPlugin() returned empty path")
	}

	b, err := os.ReadFile(filePath) //nolint:gosec // Test code reads from temp dir.
	if err != nil {
		t.Fatalf("failed reading plugin file: %v", err)
	}
	if string(b) != expectedContent {
		t.Fatalf("content mismatch: got %q want %q", string(b), expectedContent)
	}
}

func TestExtractPluginBinary_MultipleSOFiles(t *testing.T) {
	// Create an archive with two .so files. The first .so encountered in the
	// tar stream should be the one extracted.
	buf := bytes.NewBuffer(nil)
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)

	first := "first-plugin-content"
	second := "second-plugin-content"

	// Write foo.so first, then bar.so.
	for _, entry := range []struct {
		name    string
		content string
	}{
		{"foo.so", first},
		{"bar.so", second},
	} {
		if err := tw.WriteHeader(&tar.Header{
			Name: entry.name,
			Size: int64(len(entry.content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, entry.content); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()

	dest := filepath.Join(t.TempDir(), "plugin.so")
	if err := extractPluginBinary(buf, dest); err != nil {
		t.Fatalf("extractPluginBinary failed: %v", err)
	}
	b, err := os.ReadFile(dest) //nolint:gosec // Test code reads from temp dir.
	if err != nil {
		t.Fatalf("failed to read written plugin: %v", err)
	}
	// foo.so appears first in the tar stream, so it should be extracted.
	if string(b) != first {
		t.Fatalf("got %q, want %q (first .so in tar stream)", string(b), first)
	}
}

// newMockLayer creates a mock OCI layer with the given media type and file contents.
func newMockLayer(mediaType types.MediaType, contents map[string][]byte) (v1.Layer, error) {
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
		&mockLayer{
			raw:       b.Bytes(),
			mediaType: mediaType,
		},
	)
}

type mockLayer struct {
	raw       []byte
	mediaType types.MediaType
}

func (r *mockLayer) DiffID() (v1.Hash, error) { return v1.Hash{}, nil }
func (r *mockLayer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewBuffer(r.raw)), nil
}
func (r *mockLayer) MediaType() (types.MediaType, error) { return r.mediaType, nil }
