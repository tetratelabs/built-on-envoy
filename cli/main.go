// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Built On Envoy CLI (boe) - Work with Envoy extensions.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/cmd"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

type CLI struct {
	List      cmd.List      `cmd:"" help:"List available extensions"`
	Run       cmd.Run       `cmd:"" help:"Run Envoy with extensions"`
	GenConfig cmd.GenConfig `cmd:"" help:"Generate Envoy configuration with extensions"`
	Push      cmd.Push      `cmd:"" help:"Push an extension to an OCI registry"`
	Pull      cmd.Pull      `cmd:"" help:"Pull an extension from an OCI registry"`

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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	kongCtx := kong.Parse(&CLI{},
		kong.Name("boe"),
		kong.Description("Built On Envoy CLI - Discover, run, and build custom filters with zero friction"),
		kong.UsageOnError(),
		kong.BindTo(ctx, (*context.Context)(nil)), // Bind it so it can be injected into commands
	)

	kongCtx.FatalIfErrorf(kongCtx.Run(ctx))
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
