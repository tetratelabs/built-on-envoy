// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package envoy provides functionality to run Envoy using func-e.
package envoy

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	funce "github.com/tetratelabs/func-e"
	"github.com/tetratelabs/func-e/api"
	"github.com/tetratelabs/func-e/experimental/admin"

	"github.com/tetratelabs/envoy-ecosystem/cli/internal/xdg"
)

// defaultLogLevel is the default Envoy component log level.
const defaultLogLevel = "error"

// Runner handles running Envoy via func-e
type Runner struct {
	// EnvoyVersion specifies the Envoy version to run. If empty, func-e's default version is used.
	EnvoyVersion string
	// LogLevel specifies the Envoy component log level.
	LogLevel string
	// Dirs specifies XDG directories for func-e
	Dirs *xdg.Directories
	// RunID specifies the run identifier for this invocation.
	RunID string
	// ListenPort is the port for Envoy listener to accept incoming traffic.
	ListenPort int
	// AdminPort is the port for Envoy admin interface.
	AdminPort int
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

	// Define startup hook that will be called when Envoy admin is ready
	start := time.Now()
	startupHook := func(_ context.Context, adminClient admin.AdminClient, _ string) error {
		startDuration := time.Since(start).Round(100 * time.Millisecond)
		_, _ = fmt.Fprintf(os.Stderr, "Envoy listening on http://localhost:%d (admin http://localhost:%d) after %v\n",
			r.ListenPort, adminClient.Port(), startDuration)
		return nil
	}

	// Build func-e options
	opts := []api.RunOption{
		api.Out(os.Stdout),
		api.EnvoyOut(os.Stdout),
		api.EnvoyErr(os.Stderr),
		api.ConfigHome(r.Dirs.ConfigHome),
		api.DataHome(r.Dirs.DataHome),
		api.StateHome(r.Dirs.StateHome),
		api.RuntimeDir(r.Dirs.RuntimeDir),
		api.RunID(r.RunID),
		admin.WithStartupHook(startupHook),
	}
	if r.EnvoyVersion != "" {
		opts = append(opts, api.EnvoyVersion(r.EnvoyVersion))
	}

	config, err := RenderConfig(ConfigTemplateParams{
		AdminPort:    r.AdminPort,
		ListenerPort: r.ListenPort,
	})
	if err != nil {
		return err
	}

	// Run Envoy with embedded config
	baseLevel, componentLevels := parseLogLevels(r.LogLevel)
	args := []string{"--config-yaml", config, "--log-level", baseLevel}
	if componentLevels != "" {
		args = append(args, "--component-log-level", componentLevels)
	}

	return funce.Run(ctx, args, opts...)
}

// parseLogLevels parses a log level string in the format "component:level,component2:level".
// It extracts the "all" component (if present) for the --log-level flag and returns the
// remaining components for --component-log-level. If "all" is not specified, it defaults
// to DefaultLogLevel.
func parseLogLevels(logLevel string) (string, string) {
	if logLevel == "" {
		return defaultLogLevel, ""
	}

	var (
		baseLevel       = defaultLogLevel
		componentLevels []string
	)
	for part := range strings.SplitSeq(logLevel, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if after, ok := strings.CutPrefix(part, "all:"); ok {
			baseLevel = after
		} else {
			componentLevels = append(componentLevels, part)
		}
	}

	return baseLevel, strings.Join(componentLevels, ",")
}
