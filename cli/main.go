// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main implements the entry point for the Built On Envoy CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/cmd"
)

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

	kongCtx := kong.Parse(cmd.NewCLI(),
		kong.Name(cmd.CLIName),
		kong.Description("Built On Envoy CLI - Discover, run, and build custom filters with zero friction"),
		kong.UsageOnError(),
		kong.BindTo(ctx, (*context.Context)(nil)), // Bind it so it can be injected into commands
		cmd.Vars,
	)

	kongCtx.FatalIfErrorf(kongCtx.Run(ctx))
}
