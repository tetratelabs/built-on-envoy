// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package cmd contains the CLI commands
package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
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
	Clean     Clean     `cmd:"" help:"Clean cache directories"`
	Version   Version   `cmd:"" help:"Print version information"`

	// Global XDG flags
	ConfigHome  string `name:"config-home" env:"BOE_CONFIG_HOME" help:"Configuration files directory. Defaults to ~/.config/boe" type:"path" default:"~/.config/boe"`
	DataHome    string `name:"data-home" env:"BOE_DATA_HOME" help:"Downloaded Envoy binaries directory. Defaults to ~/.local/share/boe" type:"path" default:"~/.local/share/boe"`
	StateHome   string `name:"state-home" env:"BOE_STATE_HOME" help:"Persistent state and logs directory. Defaults to ~/.local/state/boe" type:"path" default:"~/.local/state/boe"`
	RuntimeDir  string `name:"runtime-dir" env:"BOE_RUNTIME_DIR" help:"Ephemeral runtime files directory. Defaults to /tmp/boe-$UID" type:"path"`
	BoeLogLevel string `name:"boe-log-level" env:"BOE_LOG_LEVEL" help:"Log level for the CLI. Defaults to debug" enum:"debug,info,warn,error" default:"debug"`
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

	dirs := &xdg.Directories{
		ConfigHome: c.ConfigHome,
		DataHome:   c.DataHome,
		StateHome:  c.StateHome,
		RuntimeDir: c.RuntimeDir,
	}
	ctx.Bind(dirs)

	logger, err := initLogger(dirs, c.BoeLogLevel)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	ctx.Bind(logger)

	return nil
}

// Version command
type Version struct {
	output io.Writer `kong:"-"` // Internal field for testing
}

// Help provides detailed help for the version command.
func (v *Version) Help() string {
	return "Print the version information for the Built On Envoy CLI."
}

// Run executes the version command, printing the version information to the output.
func (v *Version) Run() error {
	out := v.output
	if out == nil {
		out = os.Stdout
	}
	_, _ = fmt.Fprintf(out, "Built On Envoy CLI: %s\n", internal.ParseVersion())
	return nil
}

// initLogger initializes the logger to write to a file in the runtime directory with the specified log level.
func initLogger(dirs *xdg.Directories, level string) (*slog.Logger, error) {
	if err := os.MkdirAll(dirs.StateHome, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	logFile, err := os.OpenFile(filepath.Join(dirs.StateHome, "boe.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	var levelVar slog.LevelVar
	if err := levelVar.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid BOE log level: %w", err)
	}

	handler := slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: &levelVar})
	logger := slog.New(handler)

	return logger, nil
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
