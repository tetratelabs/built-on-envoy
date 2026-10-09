// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func parseDebug(t *testing.T, args ...string) (*Debug, error) {
	t.Helper()
	t.Setenv("BOE_RUN_DOCKER", "false")

	var cli struct {
		Debug Debug `cmd:""`
	}
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Exit(func(int) {}),
		kong.BindTo(t.Context(), (*context.Context)(nil)),
		kong.Bind(&xdg.Directories{}),
		Vars,
	)
	require.NoError(t, err)

	// Positions of the extensions are computed from os.Args in BeforeResolve.
	originalArgs := os.Args
	os.Args = append([]string{"boe", "debug"}, args...)
	t.Cleanup(func() { os.Args = originalArgs })

	_, err = parser.Parse(append([]string{"debug"}, args...))
	return &cli.Debug, err
}

func TestParseCmdDebug(t *testing.T) {
	d, err := parseDebug(t, "--local", "../../extensions/composer/opa", "--config", `{"a":"b"}`)
	require.NoError(t, err)
	require.Equal(t, uint32(2345), d.DebugPort)
	require.Empty(t, d.DebugServerPath)
	require.Equal(t, envoy.DebuggerDelve, d.debugger)
	opa, err := filepath.Abs("../../extensions/composer/opa")
	require.NoError(t, err)
	require.Equal(t, []string{opa}, d.RunOpts.Local)
	require.Equal(t, []string{`{"a":"b"}`}, d.RunOpts.Configs)
	require.Equal(t, "all:error", d.RunOpts.LogLevel)
	require.NotEmpty(t, d.RunOpts.extensionPositions.local)

	d, err = parseDebug(t, "--local", "../../extensions/composer/example", "--debug-port", "40000", "--debug-server-path", "/usr/bin/dlv")
	require.NoError(t, err)
	require.Equal(t, uint32(40000), d.DebugPort)
	require.Equal(t, "/usr/bin/dlv", d.DebugServerPath)
}

func TestParseCmdDebugDockerImageVersion(t *testing.T) {
	// The image version can be set without --docker as debug may run Envoy in a container.
	d, err := parseDebug(t, "--local", "../../extensions/composer/example", "--docker-image-version", "dev")
	require.NoError(t, err)
	require.Equal(t, "dev", d.RunOpts.Docker.ImageVersion)
}

func TestDebugValidate(t *testing.T) {
	goExt := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goExt, "manifest.yaml"), []byte(validGoManifest("test-go")), 0o600))

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"remote extension", []string{"--local", "../../extensions/composer/opa", "--extension", "cors"}, errDebugRemoteExtension.Error()},
		{"no local extension", nil, errDebugNoLocal.Error()},
		{"lua extension", []string{"--local", "../../extensions/example-lua"}, errDebugExtensionType.Error()},
		{"rust extension", []string{"--local", "../../extensions/ip-restriction"}, ""},
		{"go and rust", []string{"--local", "../../extensions/composer/opa", "--local", "../../extensions/ip-restriction"}, errDebugMixedTypes.Error()},
		{"docker with rust", []string{"--local", "../../extensions/ip-restriction", "--docker"}, "--docker is not supported for Rust extensions"},
		{"rebuild-dlv with rust", []string{"--local", "../../extensions/ip-restriction", "--rebuild-dlv"}, "--rebuild-dlv only applies to Go extensions"},
		{"invalid manifest", []string{"--local", t.TempDir()}, errFailedToLoadLocalManifest.Error()},
		{"composer sub-extension", []string{"--local", "../../extensions/composer/opa"}, ""},
		{"go extension", []string{"--local", goExt}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDebug(t, tt.args...)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestDebugValidateRebuildDelve(t *testing.T) {
	d, err := parseDebug(t, "--local", "../../extensions/composer/example", "--rebuild-dlv")
	require.NoError(t, err)
	require.True(t, d.RebuildDelve)

	_, err = parseDebug(t, "--local", "../../extensions/composer/example", "--rebuild-dlv", "--debug-server-path", "/usr/bin/dlv")
	require.ErrorContains(t, err, "--rebuild-dlv and --debug-server-path are mutually exclusive")
}

func TestDebugInContainer(t *testing.T) {
	// Go extensions are debugged in a container on non-Linux platforms.
	require.False(t, (&Debug{goos: "linux", debugger: envoy.DebuggerDelve}).inContainer())
	require.True(t, (&Debug{goos: "darwin", debugger: envoy.DebuggerDelve}).inContainer())
	require.True(t, (&Debug{goos: "linux", debugger: envoy.DebuggerDelve, RunOpts: Run{Docker: DockerFlags{Enabled: true}}}).inContainer())
	// Native extensions are always debugged natively, with a GDB remote protocol server.
	require.False(t, (&Debug{goos: "linux", debugger: envoy.DebuggerGDBRemote}).inContainer())
	require.False(t, (&Debug{goos: "darwin", debugger: envoy.DebuggerGDBRemote}).inContainer())
}

func TestDebugValidateEnvoyPathInContainer(t *testing.T) {
	d := &Debug{goos: "darwin", RunOpts: Run{
		Local: []string{"../../extensions/composer/opa"},
		Envoy: EnvoyFlags{Path: "/usr/local/bin/envoy"},
	}}
	require.ErrorContains(t, d.Validate(), "--envoy-path is not supported when debugging Go extensions in a container")

	// Rust extensions are debugged natively, so a custom Envoy binary can be used.
	d.RunOpts.Local = []string{"../../extensions/ip-restriction"}
	require.NoError(t, d.Validate())
	d.RunOpts.Local = []string{"../../extensions/composer/opa"}

	d.goos = "linux"
	require.NoError(t, d.Validate())
}

// validGoManifest returns a valid standalone Go extension manifest.
func validGoManifest(name string) string {
	return `name: ` + name + `
version: 1.0.0
composerVersion: 1.0.0
categories:
  - Network
author: Test
description: Test extension
longDescription: Test long description
type: go
tags:
  - test
license: Apache-2.0
examples:
  - title: Test
    description: Test example
    code: boe run --extension ` + name
}

func TestSymlinkSubstitutePath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	realDir := filepath.Join(dir, "real", "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(realDir, "ext"), 0o750))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(realDir, link))

	from, to, ok := symlinkSubstitutePath(filepath.Join(link, "ext"))
	require.True(t, ok)
	require.Equal(t, realDir, from)
	require.Equal(t, link, to)

	_, _, ok = symlinkSubstitutePath(filepath.Join(realDir, "ext"))
	require.False(t, ok)
}

func TestDebugRun(t *testing.T) {
	// Run Envoy natively and fail before starting it, to check the run options are propagated.
	d := &Debug{goos: "linux", RunOpts: Run{
		Local: []string{"../../extensions/composer/example"},
		Envoy: EnvoyFlags{Path: filepath.Join(t.TempDir(), "envoy")},
	}}
	err := d.Run(t.Context(), &xdg.Directories{DataHome: t.TempDir()}, internaltesting.NewTLogger(t))
	require.ErrorContains(t, err, "specified Envoy binary not found")
	require.False(t, d.RunOpts.Docker.Enabled)
}

func TestSymlinkHints(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	realDir := filepath.Join(dir, "real")
	require.NoError(t, os.MkdirAll(filepath.Join(realDir, "ext1"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(realDir, "ext2"), 0o750))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(realDir, link))

	// Paths through the same symlink produce a single hint, and paths without symlinks produce none.
	hints := symlinkHints([]string{
		filepath.Join(link, "ext1"),
		filepath.Join(link, "ext2"),
		filepath.Join(realDir, "ext1"),
	})
	require.Len(t, hints, 1)
	require.Contains(t, hints[0], filepath.Join(link, "ext1")+" resolves to "+filepath.Join(realDir, "ext1"))
	require.Contains(t, hints[0], `"substitutePath": [{ "from": "`+realDir+`", "to": "`+link+`" }]`)

	require.Empty(t, symlinkHints([]string{filepath.Join(dir, "missing")}))
}

func TestMustEvalSymlinks(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(dir, link))

	require.Equal(t, dir, mustEvalSymlinks(link))
	missing := filepath.Join(dir, "missing")
	require.Equal(t, missing, mustEvalSymlinks(missing))
}
