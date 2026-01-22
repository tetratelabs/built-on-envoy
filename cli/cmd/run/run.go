// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package run implements the 'ee run' command.
package run

import (
	"fmt"
	"time"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/envoy-ecosystem/cli/internal/envoy"
	"github.com/tetratelabs/envoy-ecosystem/cli/internal/xdg"
)

// Cmd represents the run command
type Cmd struct {
	EnvoyVersion string `help:"Envoy version to use (e.g., 1.31.0)" env:"ENVOY_VERSION"`
	LogLevel     string `help:"Envoy component log level (default: all:error)" short:"l" default:"all:error"`
	RunID        string `name:"run-id" env:"EE_RUN_ID" help:"Run identifier for this invocation. Defaults to timestamp-based ID or $EE_RUN_ID. Use '0' for Docker/Kubernetes."`
	ListenPort   int    `help:"Port for Envoy listener to accept incoming traffic  (default: 10000)" default:"10000"`
	AdminPort    int    `help:"Port for Envoy admin interface (default: 9901)" default:"9901"`
}

// BeforeApply is called by Kong before applying defaults to set computed default values.
func (c *Cmd) BeforeApply(_ *kong.Context) error {
	// Set RunID default if not provided
	if c.RunID == "" {
		c.RunID = generateRunID(time.Now())
	}
	return nil
}

// Run executes the run command
func (c *Cmd) Run(dirs *xdg.Directories) error {
	runner := &envoy.Runner{
		EnvoyVersion: c.EnvoyVersion,
		LogLevel:     c.LogLevel,
		Dirs:         dirs,
		RunID:        c.RunID,
		ListenPort:   c.ListenPort,
		AdminPort:    c.AdminPort,
	}
	return runner.Run()
}

// generateRunID generates a unique run identifier based on the current time.
// Defaults to the same convention as func-e: "YYYYMMDD_HHMMSS_UUU" format.
// Last 3 digits of microseconds to allow concurrent runs.
func generateRunID(now time.Time) string {
	micro := now.Nanosecond() / 1000 % 1000
	return fmt.Sprintf("%s_%03d", now.Format("20060102_150405"), micro)
}
