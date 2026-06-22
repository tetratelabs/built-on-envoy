// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestParseCmdUIHelp(t *testing.T) {
	var cli struct {
		UI UI `cmd:"" help:"Start the web UI"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"ui", "--help"})

	expected := fmt.Sprintf(`Usage: boe ui [flags]

Start the web UI

%s
Flags:
  -h, --help                     Show context-sensitive help.

      --port=18000               HTTP server port.
      --log-level="all:error"    Envoy component log level ($ENVOY_LOG_LEVEL).
      --envoy-version=STRING     Envoy version to use (e.g., 1.31.0, dev,
                                 dev-latest) ($ENVOY_VERSION)
      --envoy-path=STRING        Path to a custom Envoy binary. Skips Envoy
                                 download and version selection ($ENVOY_PATH).
      --local=LOCAL              Path to a directory containing a local
                                 Extension to enable.
      --dev                      Whether to allow downloading dev versions of
                                 extensions (with -dev suffix). By default,
                                 only stable versions are allowed.
      --cluster=CLUSTER,...      Optional additional Envoy cluster provided in
                                 the host:tlsPort pattern.
      --cluster-insecure=CLUSTER-INSECURE,...
                                 Optional additional Envoy cluster (with TLS
                                 transport disabled) provided in the host:port
                                 pattern.
      --cluster-json=CLUSTER-JSON
                                 Optional additional Envoy cluster providing the
                                 complete cluster config in JSON format.
      --test-upstream-host=STRING
                                 Hostname for the test upstream
                                 cluster. Mutually exclusive with
                                 --test-upstream-cluster. Defaults to
                                 "httpbin.org".
      --test-upstream-cluster=STRING
                                 Name of an existing configured cluster to
                                 use as the test upstream. The cluster must be
                                 configured via --cluster, --cluster-insecure,
                                 or --cluster-json. Mutually exclusive with
                                 --test-upstream-host.
      --docker                   Run Envoy as a Docker container instead of
                                 using func-e ($BOE_RUN_DOCKER).
      --pull="missing"           Pull policy for the BOE Docker image (missing,
                                 always, never). Only applicable when running
                                 with --docker.
      --docker-image-version=STRING
                                 Override the BOE Docker image tag to use when
                                 running with --docker. By default, the image
                                 version matches the BOE version.
`, wrapHelp(uiHelp))

	require.Equal(t, expected, buf.String())
}

func TestUIRun_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	ctx, cancel := context.WithCancel(t.Context())

	u := &UI{
		Port:            port,
		LogLevel:        "all:error",
		openBrowserFunc: func(string) error { return nil },
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- u.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	cancel()

	require.NoError(t, <-errCh)
}

func TestUIRun_InvalidLocalExtension(t *testing.T) {
	tmpDir := t.TempDir()

	u := &UI{Local: []string{tmpDir}}
	err := u.Run(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.ErrorContains(t, err, "failed to read local extension manifest")
}

func TestUIRun_BrowserOpenFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	ctx, cancel := context.WithCancel(t.Context())

	u := &UI{
		Port:            port,
		LogLevel:        "all:error",
		openBrowserFunc: func(string) error { return fmt.Errorf("no display") },
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- u.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	cancel()

	require.NoError(t, <-errCh)
}

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		os      string
		url     string
		wantCmd []string
		wantErr bool
	}{
		{
			os:      "darwin",
			url:     "http://localhost:18000",
			wantCmd: []string{"open", "http://localhost:18000"},
		},
		{
			os:      "linux",
			url:     "http://localhost:18000",
			wantCmd: []string{"xdg-open", "http://localhost:18000"},
		},
		{
			os:      "windows",
			url:     "http://localhost:18000",
			wantCmd: []string{"rundll32", "url.dll,FileProtocolHandler", "http://localhost:18000"},
		},
		{
			os:      "unsupported",
			url:     "http://localhost:18000",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.os, func(t *testing.T) {
			cmd, err := browserCommand(tt.os, tt.url)
			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.wantCmd, cmd)
		})
	}
}
