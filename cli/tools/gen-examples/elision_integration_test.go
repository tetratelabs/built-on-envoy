// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// The public generator lifecycle must preserve reviewed excerpts while refusing failed executions and stale retained output.
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestGenerateElidedOutput(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "examples"), 0o700))
	scriptPath := filepath.Join(dir, "examples", "run.sh")
	manifestPath := filepath.Join(dir, "manifest.yaml")
	t.Setenv("GEN_BOE_ARGS_FILE", filepath.Join(t.TempDir(), "boe-args"))
	manifest := extensions.Manifest{
		Name: "example-elision", Version: "1.0.0", Categories: []string{"Examples"},
		Author: "Test", Description: "Elision test", LongDescription: "Elision test.",
		Type: extensions.TypeLua, Lua: &extensions.Lua{Inline: "function envoy_on_request(handle) end"},
		Tags: []string{"example"}, License: "Apache-2.0",
		Examples: []extensions.Example{{
			Title: "Elided", Description: "Retains meaningful output.", Config: mapConfig(map[string]any{}),
			Commands:        []extensions.ExampleCommand{{Argv: []string{"sh", "run.sh"}}},
			VolatileHeaders: []string{"x-time"}, ElideOutput: true,
		}},
	}
	writeManifest := func() {
		data, err := yaml.Marshal(&manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(manifestPath, data, 0o600))
	}
	readManifest := func() []byte {
		data, err := os.ReadFile(manifestPath) // #nosec G304 -- this manifest lives in the test's temporary directory.
		require.NoError(t, err)
		require.NoError(t, yaml.Unmarshal(data, &manifest))
		return data
	}
	writeScript := func(output string) {
		require.NoError(t, os.WriteFile(scriptPath, []byte("printf "+shellQuote(output)+"\n"), 0o600))
	}
	writeScript("HTTP/1.1 200 OK\\r\\nX-Time: 12.3ms\\r\\n\\r\\nfirst\\r\\nhidden one\\r\\nhidden two\\r\\nlast\\r\\n")
	writeManifest()
	opts := &options{extensions: stringList{dir}, boe: fakeBoe(t), envoyVersion: "1.38.0", timeout: 5 * time.Second}
	run := func() error { return generate(context.Background(), opts, &bytes.Buffer{}, &bytes.Buffer{}) }
	require.NoError(t, run())
	readManifest()
	manifest.Examples[0].Code = strings.ReplaceAll(manifest.Examples[0].Code, "hidden one\nhidden two\n", "...\n")
	writeManifest()
	before := readManifest()
	opts.check = true
	require.ErrorContains(t, run(), "stale")
	require.Equal(t, before, readManifest(), "check must not repair metadata or rewrite examples")
	opts.check = false
	require.NoError(t, run())
	reviewed := readManifest()
	require.Contains(t, manifest.Examples[0].Code, "first\n...\nlast\n")
	writeScript("HTTP/1.1 200 OK\\r\\nX-Time: 98.765ms\\r\\n\\r\\nfirst\\r\\nchanged hidden output\\nlast\\r\\n")
	opts.check = true
	require.NoError(t, run())
	require.Equal(t, reviewed, readManifest())
	opts.check = false
	require.NoError(t, run())
	require.Equal(t, reviewed, readManifest(), "updates also preserve equivalent elided output")

	manifest.Examples[0].ElideOutput = false
	writeManifest()
	opts.check = true
	require.ErrorContains(t, run(), "stale")
	manifest.Examples[0].ElideOutput = true
	writeManifest()
	before = readManifest()
	require.NoError(t, os.WriteFile(scriptPath, []byte("printf 'ignored failure\\n'; exit 9\n"), 0o600))
	opts.check = false
	require.ErrorContains(t, run(), "exit")
	require.Equal(t, before, readManifest(), "elision cannot hide command failures")

	writeScript("HTTP/1.1 200 OK\\r\\nX-Time: 98.765s\\r\\n\\r\\nfirst\\r\\nchanged hidden output\\r\\nlast\\r\\n")
	opts.check = true
	require.ErrorContains(t, run(), "stale")
	require.Equal(t, before, readManifest())
	opts.check = false
	require.NoError(t, run())
	readManifest()
	require.Contains(t, manifest.Examples[0].Code, "X-Time: 98.765s")
	require.Contains(t, manifest.Examples[0].Code, "changed hidden output")
	require.NotContains(t, manifest.Examples[0].Code, "...\n", "retained drift expands the real capture for review")
}
