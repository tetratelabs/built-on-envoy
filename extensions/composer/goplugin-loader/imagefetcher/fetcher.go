// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package imagefetcher

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Option configures the OCI image fetcher.
type Option struct {
	PullSecret []byte // Docker config JSON content for registry auth
	Insecure   bool   // Allow HTTP and self-signed TLS
	CacheDir   string // Root directory for caching fetched plugin binaries
	Platform   string // Target platform (e.g. "linux/arm64"). Defaults to runtime OS/arch.
}

// maxPluginSize is the upper bound for a plugin's size (512 MB).
const maxPluginSize int64 = 512 * 1024 * 1024

// FetchPlugin fetches a Go plugin binary from an OCI registry and returns the
// local file path. The ref should be a plain OCI reference (e.g.
// "registry.example.com/plugins/myplugin:v1") without an "oci://" scheme prefix.
// Results are cached by digest so a given image is only downloaded once.
func FetchPlugin(ctx context.Context, ref string, pluginName string, opt Option) (string, error) {
	remoteOpts := buildRemoteOpts(ctx, opt)

	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("failed to parse OCI reference: %w", err)
	}

	// HTTPS-first with HTTP fallback, inspired by Helm.
	desc, err := remote.Get(parsedRef, remoteOpts...)
	if err != nil && strings.Contains(err.Error(), "server gave HTTP response") {
		parsedRef, err = name.ParseReference(ref, name.Insecure)
		if err == nil {
			desc, err = remote.Get(parsedRef, remoteOpts...)
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}

	img, err := desc.Image()
	if err != nil {
		return "", fmt.Errorf("failed to fetch image: %w", err)
	}

	d, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("failed to get image digest: %w", err)
	}

	destPath := filepath.Join(opt.CacheDir, pluginName, d.Hex+".so")
	if _, err := os.Stat(destPath); err == nil {
		return destPath, nil
	}

	if err := fetchPluginBinary(img, destPath); err != nil {
		return "", fmt.Errorf("failed to fetch plugin binary: %w", err)
	}
	return destPath, nil
}

func buildRemoteOpts(ctx context.Context, opt Option) []remote.Option {
	remoteOpts := make([]remote.Option, 0, 4)

	if opt.PullSecret == nil {
		remoteOpts = append(remoteOpts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	} else {
		remoteOpts = append(remoteOpts, remote.WithAuthFromKeychain(&pluginKeyChain{data: opt.PullSecret}))
	}

	if opt.Insecure {
		t := remote.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // User explicitly opted into insecure mode.
		}
		remoteOpts = append(remoteOpts, remote.WithTransport(t))
	}

	p := v1.Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH}
	if opt.Platform != "" {
		parts := strings.SplitN(opt.Platform, "/", 2)
		if len(parts) == 2 {
			p = v1.Platform{OS: parts[0], Architecture: parts[1]}
		}
	}
	remoteOpts = append(remoteOpts,
		remote.WithPlatform(p),
		remote.WithContext(ctx),
	)

	return remoteOpts
}

func fetchPluginBinary(img v1.Image, destPath string) error {
	manifest, err := img.Manifest()
	if err != nil {
		return fmt.Errorf("failed to retrieve manifest: %w", err)
	}

	if manifest.MediaType == types.DockerManifestSchema2 {
		return extractImageLayer(img, destPath, types.DockerLayer)
	}

	return extractImageLayer(img, destPath, types.OCILayer)
}

// extractImageLayer extracts the plugin binary from the last layer of an image,
// validating that the layer has the expected media type.
func extractImageLayer(img v1.Image, destPath string, expectedMediaType types.MediaType) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("failed to fetch layers: %w", err)
	}
	if len(layers) == 0 {
		return errors.New("number of layers must be greater than zero")
	}

	layer := layers[len(layers)-1]
	mt, err := layer.MediaType()
	if err != nil {
		return fmt.Errorf("failed to get media type: %w", err)
	}
	if mt != expectedMediaType {
		return fmt.Errorf("invalid media type %s (expect %s)", mt, expectedMediaType)
	}

	r, err := layer.Compressed()
	if err != nil {
		return fmt.Errorf("failed to get layer content: %w", err)
	}
	defer r.Close() //nolint:errcheck // Best-effort cleanup on read stream.

	return extractPluginBinary(r, destPath)
}

// extractPluginBinary extracts the first .so file from a tar.gz stream and
// writes it atomically to destPath.
func extractPluginBinary(r io.Reader, destPath string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to parse layer as tar.gz: %w", err)
	}
	defer gr.Close() //nolint:errcheck // Best-effort cleanup on gzip reader.

	tr := tar.NewReader(io.LimitReader(gr, maxPluginSize))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		if filepath.Ext(h.Name) != ".so" {
			continue
		}

		if !h.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular file: %s", h.Name)
		}
		if h.Size < 0 || h.Size > maxPluginSize {
			return fmt.Errorf("plugin too large or invalid size: %d", h.Size)
		}

		if mkdirErr := os.MkdirAll(filepath.Dir(destPath), 0o750); mkdirErr != nil {
			return fmt.Errorf("failed to create plugin directory: %w", mkdirErr)
		}

		tmpf, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".*")
		if err != nil {
			return err
		}
		defer func() {
			_ = tmpf.Close()
			_ = os.Remove(tmpf.Name())
		}()

		if _, err := io.CopyN(tmpf, tr, h.Size); err != nil {
			return fmt.Errorf("failed to copy plugin to disk: %w", err)
		}

		perm := h.FileInfo().Mode().Perm()
		if perm == 0 {
			perm = 0o755
		}
		if err := tmpf.Chmod(perm); err != nil {
			return err
		}
		if err := tmpf.Sync(); err != nil {
			return err
		}
		if err := os.Rename(tmpf.Name(), destPath); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf(".so file not found in the archive")
}
