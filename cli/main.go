// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Envoy Ecosystem CLI (ee) - Work with Envoy extensions.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/envoy-ecosystem/cli/cmd/run"
	"github.com/tetratelabs/envoy-ecosystem/cli/internal/xdg"
)

type CLI struct {
	Run run.Cmd `cmd:"" help:"Run Envoy with extensions"`

	// Global XDG flags
	ConfigHome string `name:"config-home" env:"EE_CONFIG_HOME" help:"Configuration files directory. Defaults to ~/.config/ee" type:"path" default:"~/.config/ee"`
	DataHome   string `name:"data-home" env:"EE_DATA_HOME" help:"Downloaded Envoy binaries directory. Defaults to ~/.local/share/ee" type:"path" default:"~/.local/share/ee"`
	StateHome  string `name:"state-home" env:"EE_STATE_HOME" help:"Persistent state and logs directory. Defaults to ~/.local/state/ee" type:"path" default:"~/.local/state/ee"`
	RuntimeDir string `name:"runtime-dir" env:"EE_RUNTIME_DIR" help:"Ephemeral runtime files directory. Defaults to /tmp/ee-$UID" type:"path"`
}

// BeforeApply is called by Kong before applying defaults to set XDG directory defaults.
func (c *CLI) BeforeApply(ctx *kong.Context) error {
	// Expand paths unconditionally (handles ~/, env vars, and converts to absolute)
	c.ConfigHome = expandPath(c.ConfigHome)
	c.DataHome = expandPath(c.DataHome)
	c.StateHome = expandPath(c.StateHome)

	if c.RuntimeDir == "" {
		c.RuntimeDir = fmt.Sprintf("/tmp/ee-%d", os.Getuid())
	}
	c.RuntimeDir = expandPath(c.RuntimeDir)

	ctx.Bind(&xdg.Directories{
		ConfigHome: c.ConfigHome,
		DataHome:   c.DataHome,
		StateHome:  c.StateHome,
		RuntimeDir: c.RuntimeDir,
	})

	return nil
}

func main() {
	ctx := kong.Parse(&CLI{},
		kong.Name("ee"),
		kong.Description("Envoy Ecosystem CLI - Discover, run, and build custom filters with zero friction"),
		kong.UsageOnError(),
	)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
	os.Exit(0)
}

// expandPath expands environment variables and tilde in paths, then converts to absolute path.
// Returns empty string if input is empty.
// Replaces ~/  with ${HOME}/ before expanding environment variables.
func expandPath(path string) string {
	if path == "" {
		return ""
	}

	// Replace ~/ with ${HOME}/
	if strings.HasPrefix(path, "~/") {
		path = "${HOME}/" + path[2:]
	}

	// Expand environment variables
	expanded := os.ExpandEnv(path)

	// Convert to absolute path
	abs, err := filepath.Abs(expanded)
	if err != nil {
		// If we can't get absolute path, return expanded path
		return expanded
	}

	return abs
}
