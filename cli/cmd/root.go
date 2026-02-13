// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package cmd contains the CLI commands
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// NewCLI creates a new instance of the CLI with default values.
func NewCLI() *CLI {
	return &CLI{}
}

// CLIName is the name of the CLI binary.
const CLIName = "boe"

// CLI is the root command for the Built On Envoy CLI.
type CLI struct {
	List      List      `cmd:"" help:"List available extensions"`
	Run       Run       `cmd:"" help:"Run Envoy with extensions"`
	GenConfig GenConfig `cmd:"" help:"Generate Envoy configuration with extensions"`
	Create    Create    `cmd:"" help:"Create a new extension template"`

	// Global XDG flags
	ConfigHome string `name:"config-home" env:"BOE_CONFIG_HOME" help:"Configuration files directory. Defaults to ~/.config/boe" type:"path" default:"~/.config/boe"`
	DataHome   string `name:"data-home" env:"BOE_DATA_HOME" help:"Downloaded Envoy binaries directory. Defaults to ~/.local/share/boe" type:"path" default:"~/.local/share/boe"`
	StateHome  string `name:"state-home" env:"BOE_STATE_HOME" help:"Persistent state and logs directory. Defaults to ~/.local/state/boe" type:"path" default:"~/.local/state/boe"`
	RuntimeDir string `name:"runtime-dir" env:"BOE_RUNTIME_DIR" help:"Ephemeral runtime files directory. Defaults to /tmp/boe-$UID" type:"path"`
}

// BeforeApply is called by Kong before applying defaults to set XDG directory defaults.
func (c *CLI) BeforeApply(ctx *kong.Context) error {
	// Expand paths unconditionally (handles ~/, env vars, and converts to absolute)
	c.ConfigHome = expandPath(c.ConfigHome)
	c.DataHome = expandPath(c.DataHome)
	c.StateHome = expandPath(c.StateHome)

	if c.RuntimeDir == "" {
		c.RuntimeDir = fmt.Sprintf("/tmp/boe-%d", os.Getuid())
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
