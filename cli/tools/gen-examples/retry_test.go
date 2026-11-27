// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These generator tests prove that retry capture accepts complete HTTP exchanges
// and preserves manifests when command options or response framing are unsafe.
package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestGenerateRetryCapture(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if errors.Is(err, syscall.EPERM) || (err != nil && strings.Contains(err.Error(), "operation not permitted")) {
		t.Skip("sandbox does not allow loopback listeners")
	}
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	bin := t.TempDir()
	curlPath := filepath.Join(bin, "curl")
	script := `#!/bin/sh
printf '%s\000' "$@" > "$GEN_CAPTURE_ARGS"
printf '%s' "$GEN_CAPTURE_STDOUT"
printf '%s' "$GEN_CAPTURE_STDERR" >&2
`
	require.NoError(t, os.WriteFile(curlPath, []byte(script), 0o600))
	require.NoError(t, os.Chmod(curlPath, 0o700)) // #nosec G302 -- executable test fixture.
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GEN_BOE_ARGS_FILE", filepath.Join(t.TempDir(), "boe-args"))
	boePath := fakeBoe(t)

	const (
		url          = "http://example.test/request"
		response     = "HTTP/1.1 503 Service Unavailable\r\nContent-Type: text/plain\r\n\r\nselected body\n"
		request      = "> GET /request HTTP/1.1\r\n> Host: example.test\r\n> \r\n"
		verbose      = "< HTTP/1.1 503 Service Unavailable\r\n< Content-Type: text/plain\r\n< \r\n"
		writeOut     = "HTTP/%{http_version} %{http_code}\\n\\n"
		selectedBody = "selected body"
	)
	tests := []struct {
		name      string
		argv      []string
		stdout    string
		stderr    string
		wantError string
		runs      bool
	}{
		{name: "combined short flags and attached values", argv: []string{"curl", "-qgsi", "-Aexample", "-XPOST", "-H", "X-Test: yes", "-d", "payload", "http://example.test/[literal]"}, stdout: response, runs: true},
		{name: "short attached write-out", argv: []string{"curl", "-q", "-o/dev/null", "-w" + writeOut, url}, stdout: "HTTP/2 503\n\n" + selectedBody, runs: true},
		{name: "long inline values", argv: []string{"curl", "--disable", "--output=/dev/null", "--write-out=" + writeOut, "--url=" + url, "--header=X-Test: yes"}, stdout: response, runs: true},
		{name: "HTTPS include suppresses proxy headers", argv: []string{"curl", "--disable", "--include", "--suppress-connect-headers", "--globoff", "--url", "https://example.test/{literal}"}, stdout: response, runs: true},
		{name: "end of options", argv: []string{"curl", "-qi", "--", url}, stdout: response, runs: true},
		{name: "matching captures", argv: []string{"curl", "-qiv", url}, stdout: response, stderr: request + verbose, runs: true},
		{name: "HTTP2 verbose capture", argv: []string{"curl", "-qv", "--http2", url}, stdout: selectedBody, stderr: "> GET /request HTTP/2\r\n> \r\n< HTTP/2 503\r\n< \r\n", runs: true},
		{name: "included informational responses", argv: []string{"curl", "-qi", url}, stdout: "HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 103 Early Hints\r\nLink: </style.css>\r\n\r\n" + response, runs: true},
		{name: "value ends a short cluster", argv: []string{"curl", "-qdvi", "-i", url}, stdout: response, runs: true},
		{name: "separate empty value", argv: []string{"curl", "-qi", "--header", "", url}, stdout: response, runs: true},
		{name: "option-looking header value", argv: []string{"curl", "-qH", "-i", url}, wantError: "requires curl --include, --verbose"},
		{name: "attached short equals is literal", argv: []string{"curl", "-q", "-o=/dev/null", "-w" + writeOut, url}, wantError: "requires --output /dev/null"},
		{name: "long abbreviations are rejected", argv: []string{"curl", "-qi", "--incl", url}, wantError: "does not support curl option --incl"},
		{name: "shell wrapper", argv: []string{"sh", "-c", "curl -qi " + url}, wantError: "direct curl executable"},
		{name: "include cannot take a value", argv: []string{"curl", "-q", "--include=true", url}, wantError: "does not support curl option --include=true"},
		{name: "missing long output value", argv: []string{"curl", "-qi", url, "--output"}, wantError: "requires a value"},
		{name: "empty header value", argv: []string{"curl", "-qi", url, "--header="}, wantError: "requires a value"},
		{name: "mixed output aliases", argv: []string{"curl", "-qv", "-o/dev/null", "--output=/dev/null", url}, wantError: "multiple curl --output"},
		{name: "mixed write-out aliases", argv: []string{"curl", "-q", "-w" + writeOut, "--write-out=" + writeOut, url}, wantError: "multiple curl --write-out"},
		{name: "missing short header value", argv: []string{"curl", "-qi", url, "-H"}, wantError: "curl option -H requires a value"},
		{name: "unknown short option", argv: []string{"curl", "-qi", "-z", url}, wantError: "does not support curl option -z"},
		{name: "invalid operand after end of options", argv: []string{"curl", "-qi", "--", "--include"}, wantError: "only an HTTP(S) URL"},
		{name: "invalid response version", argv: []string{"curl", "-qi", url}, stdout: "HTTP/1.x 503\r\n\r\n", wantError: "invalid initial response status line", runs: true},
		{name: "out of range status", argv: []string{"curl", "-qi", url}, stdout: "HTTP/1.1 600 Invalid\r\n\r\n", wantError: "outside 100-599", runs: true},
		{name: "unterminated response", argv: []string{"curl", "-qi", url}, stdout: "HTTP/1.1 503\r\nContent-Type: text/plain\r\n", wantError: "no terminating blank line", runs: true},
		{name: "invalid response header", argv: []string{"curl", "-qi", url}, stdout: "HTTP/1.1 503\r\ninvalid header\r\n\r\n", wantError: "invalid header", runs: true},
		{name: "interim without final", argv: []string{"curl", "-qi", url}, stdout: "HTTP/1.1 100 Continue\r\n\r\n", wantError: "no following final response", runs: true},
		{name: "invalid verbose method", argv: []string{"curl", "-qv", url}, stderr: "> get /request HTTP/1.1\r\n> \r\n" + verbose, wantError: "invalid initial request line", runs: true},
		{name: "proxy CONNECT response", argv: []string{"curl", "-qv", url}, stderr: "> CONNECT example.test:443 HTTP/1.1\r\n> \r\n" + verbose, wantError: "proxy CONNECT responses are not retryable", runs: true},
		{name: "invalid request headers", argv: []string{"curl", "-qv", url}, stderr: "> GET /request HTTP/1.1\r\n> invalid header\r\n> \r\n" + verbose, wantError: "invalid initial request headers", runs: true},
		{name: "missing verbose response", argv: []string{"curl", "-qv", url}, stderr: request, wantError: "initial request has no complete response", runs: true},
		{name: "invalid verbose response headers", argv: []string{"curl", "-qv", url}, stderr: request + "< HTTP/1.1 503\r\n< invalid header\r\n< \r\n", wantError: "parse curl --verbose stderr: invalid response headers", runs: true},
		{name: "multiple verbose responses", argv: []string{"curl", "-qv", url}, stderr: request + verbose + verbose, wantError: "multiple verbose responses are ambiguous", runs: true},
		{name: "multiple verbose requests", argv: []string{"curl", "-qv", url}, stderr: request + verbose + request, wantError: "multiple verbose requests are ambiguous", runs: true},
		{name: "empty verbose output", argv: []string{"curl", "-qv", url}, wantError: "no complete initial HTTP response", runs: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extensionPath := t.TempDir()
			manifestPath := filepath.Join(extensionPath, "manifest.yaml")
			argsPath := filepath.Join(t.TempDir(), "curl-args")
			afterPath := filepath.Join(t.TempDir(), "after-retry")
			t.Setenv("GEN_CAPTURE_ARGS", argsPath)
			t.Setenv("GEN_CAPTURE_STDOUT", tt.stdout)
			t.Setenv("GEN_CAPTURE_STDERR", tt.stderr)
			manifest := extensions.Manifest{
				Name: "retry-capture-test", Version: "1.0.0", Categories: []string{"Examples"},
				Author: "Test", Description: "Retry capture test", LongDescription: "Retry capture test.",
				Type: extensions.TypeLua, Lua: &extensions.Lua{Inline: "function envoy_on_request(handle) end"},
				Tags: []string{"example"}, License: "Apache-2.0",
				Examples: []extensions.Example{{
					Title: "Retry capture", Description: "Captures one complete response.",
					Config: mapConfig(map[string]any{}), Code: "original transcript\n",
					Commands: []extensions.ExampleCommand{
						{Argv: tt.argv, Retry: &extensions.ExampleRetry{HTTPStatus: 503, MaxAttempts: 1}},
						{Argv: []string{"touch", afterPath}},
					},
				}},
			}
			before, marshalErr := yaml.Marshal(&manifest)
			require.NoError(t, marshalErr)
			require.NoError(t, os.WriteFile(manifestPath, before, 0o600))
			opts := &options{extensions: stringList{extensionPath}, boe: boePath, envoyPath: "/fake/envoy", timeout: 5 * time.Second}
			generateErr := generate(context.Background(), opts, &strings.Builder{}, &strings.Builder{})
			after, readErr := os.ReadFile(manifestPath) // #nosec G304 -- temporary manifest owned by this test.
			require.NoError(t, readErr)
			if tt.wantError != "" {
				require.ErrorContains(t, generateErr, tt.wantError)
				require.Equal(t, before, after, "failed captures must preserve the manifest")
				_, statErr := os.Stat(afterPath)
				require.ErrorIs(t, statErr, os.ErrNotExist, "failed captures must stop subsequent commands")
			} else {
				require.NoError(t, generateErr)
				require.NoError(t, yaml.Unmarshal(after, &manifest))
				require.Contains(t, manifest.Examples[0].Code, selectedBody)
				require.NotContains(t, manifest.Examples[0].Code, "original transcript")
				_, statErr := os.Stat(afterPath)
				require.NoError(t, statErr)
			}
			if tt.runs {
				args, argsErr := os.ReadFile(argsPath) // #nosec G304 -- temporary argument capture owned by this test.
				require.NoError(t, argsErr)
				require.Equal(t, tt.argv[1:], strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00"), "execution must preserve argument boundaries")
			} else {
				_, statErr := os.Stat(argsPath)
				require.ErrorIs(t, statErr, os.ErrNotExist, "unsafe curl forms must be rejected before execution")
			}
		})
	}
}
