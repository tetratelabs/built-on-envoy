// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package run implements the 'ee run' command.
package run

import (
	"github.com/tetratelabs/envoy-ecosystem/cli/internal/envoy"
)

// Cmd represents the run command
type Cmd struct {
	Plugin       []string `help:"Extension to load (can be specified multiple times)" short:"p"`
	Config       string   `help:"Path to custom Envoy configuration file" type:"path"`
	EnvoyVersion string   `help:"Envoy version to use (e.g., 1.31.0)" env:"ENVOY_VERSION"`
}

// Run executes the run command
func (c *Cmd) Run() error {
	runner := &envoy.Runner{EnvoyVersion: c.EnvoyVersion}
	return runner.Run()
}
