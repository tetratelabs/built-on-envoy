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

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
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
	require.Equal(t, uint32(2345), d.DelvePort)
	require.Empty(t, d.DelvePath)
	opa, err := filepath.Abs("../../extensions/composer/opa")
	require.NoError(t, err)
	require.Equal(t, []string{opa}, d.RunOpts.Local)
	require.Equal(t, []string{`{"a":"b"}`}, d.RunOpts.Configs)
	require.Equal(t, "all:error", d.RunOpts.LogLevel)
	require.NotEmpty(t, d.RunOpts.extensionPositions.local)

	d, err = parseDebug(t, "--local", "../../extensions/composer/example", "--dlv-port", "40000", "--dlv-path", "/usr/bin/dlv")
	require.NoError(t, err)
	require.Equal(t, uint32(40000), d.DelvePort)
	require.Equal(t, "/usr/bin/dlv", d.DelvePath)
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
		{"non-go extension", []string{"--local", "../../extensions/example-lua"}, errDebugExtensionType.Error()},
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

	_, err = parseDebug(t, "--local", "../../extensions/composer/example", "--rebuild-dlv", "--dlv-path", "/usr/bin/dlv")
	require.ErrorContains(t, err, "--rebuild-dlv and --dlv-path are mutually exclusive")
}

func TestDebugInContainer(t *testing.T) {
	require.False(t, (&Debug{goos: "linux"}).inContainer())
	require.True(t, (&Debug{goos: "darwin"}).inContainer())
	require.True(t, (&Debug{goos: "linux", RunOpts: Run{Docker: DockerFlags{Enabled: true}}}).inContainer())
}

func TestDebugValidateEnvoyPathInContainer(t *testing.T) {
	d := &Debug{goos: "darwin", RunOpts: Run{
		Local: []string{"../../extensions/composer/opa"},
		Envoy: EnvoyFlags{Path: "/usr/local/bin/envoy"},
	}}
	require.ErrorContains(t, d.Validate(), "--envoy-path is not supported when debugging in a container")

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
