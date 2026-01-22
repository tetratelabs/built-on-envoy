// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package envoy provides functionality to run Envoy using func-e.
package envoy

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	funce "github.com/tetratelabs/func-e"
	"github.com/tetratelabs/func-e/api"
)

//go:embed config.yaml
var defaultConfig string

// Runner handles running Envoy via func-e
type Runner struct {
	EnvoyVersion string
}

// Run starts Envoy using func-e as a library
func (r *Runner) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Build func-e options
	opts := []api.RunOption{
		api.Out(os.Stdout),
		api.EnvoyOut(os.Stdout),
		api.EnvoyErr(os.Stderr),
	}

	if r.EnvoyVersion != "" {
		opts = append(opts, api.EnvoyVersion(r.EnvoyVersion))
	}

	// Run Envoy with embedded config
	args := []string{"--config-yaml", defaultConfig}
	err := funce.Run(ctx, args, opts...)
	if err != nil {
		return fmt.Errorf("failed to run envoy: %w", err)
	}

	return nil
}
