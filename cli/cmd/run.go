// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package cmd contains the CLI commands
package cmd

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// defaultLogLevel is the default Envoy component log level.
const defaultLogLevel = "error"

// Run represents the run command
type Run struct {
	EnvoyVersion string   `help:"Envoy version to use (e.g., 1.31.0)" env:"ENVOY_VERSION"`
	LogLevel     string   `help:"Envoy component log level (default: all:error)" short:"l" default:"all:error"`
	RunID        string   `name:"run-id" env:"BOE_RUN_ID" help:"Run identifier for this invocation. Defaults to timestamp-based ID or $BOE_RUN_ID. Use '0' for Docker/Kubernetes."`
	ListenPort   int      `help:"Port for Envoy listener to accept incoming traffic  (default: 10000)" default:"10000"`
	AdminPort    int      `help:"Port for Envoy admin interface (default: 9901)" default:"9901"`
	Extensions   []string `name:"extension" help:"Extensions to enable (by name)." sep:","`

	defaultLogLevel   string `kong:"-"` // Internal field: parsed defaut log level
	componentLogLevel string `kong:"-"` // Internal field: parsed component log levels
}

// BeforeApply is called by Kong before applying defaults to set computed default values.
func (r *Run) BeforeApply(_ *kong.Context) error {
	// Set RunID default if not provided
	if r.RunID == "" {
		r.RunID = generateRunID(time.Now())
	}
	return nil
}

// Validate is called by Kong after parsing to validate the command arguments.
func (r *Run) Validate() error {
	var err error

	r.defaultLogLevel, r.componentLogLevel, err = parseLogLevels(r.LogLevel)
	if err != nil {
		return err
	}

	for _, name := range r.Extensions {
		if _, found := extensions.Manifests[name]; !found {
			available := slices.Collect(maps.Keys(extensions.Manifests))
			sort.Strings(available)
			return fmt.Errorf("unknown extension %q; available extensions: %s", name, strings.Join(available, ","))
		}
	}

	return nil
}

// Run executes the run command
func (r *Run) Run(ctx context.Context, dirs *xdg.Directories) error {
	manifests := make([]*extensions.Manifest, 0, len(r.Extensions))
	for _, name := range r.Extensions {
		manifests = append(manifests, extensions.Manifests[name])
	}

	runner := &envoy.Runner{
		EnvoyVersion:      r.EnvoyVersion,
		DefaultLogLevel:   r.defaultLogLevel,
		ComponentLogLevel: r.componentLogLevel,
		Dirs:              dirs,
		RunID:             r.RunID,
		ListenPort:        r.ListenPort,
		AdminPort:         r.AdminPort,
		Extensions:        manifests,
	}

	return runner.Run(ctx)
}

// generateRunID generates a unique run identifier based on the current time.
// Defaults to the same convention as func-e: "YYYYMMDD_HHMMSS_UUU" format.
// Last 3 digits of microseconds to allow concurrent runs.
func generateRunID(now time.Time) string {
	micro := now.Nanosecond() / 1000 % 1000
	return fmt.Sprintf("%s_%03d", now.Format("20060102_150405"), micro)
}

// parseLogLevels parses a log level string in the format "component:level,component2:level".
// It extracts the "all" component (if present) for the --log-level flag and returns the
// remaining components for --component-log-level. If "all" is not specified, it defaults
// to DefaultLogLevel.
func parseLogLevels(logLevel string) (string, string, error) {
	if logLevel == "" {
		return defaultLogLevel, "", nil
	}

	var (
		baseLevel       = defaultLogLevel
		componentLevels []string
	)
	for part := range strings.SplitSeq(logLevel, ",") {
		component, level, found := strings.Cut(strings.TrimSpace(part), ":")
		if !found {
			return "", "", fmt.Errorf("invalid log level format %q: expected component:level", part)
		}

		component = strings.TrimSpace(component)
		level = strings.TrimSpace(level)

		if component == "" {
			return "", "", fmt.Errorf("invalid log level format %q: component cannot be empty", part)
		}
		if level == "" {
			return "", "", fmt.Errorf("invalid log level format %q: level cannot be empty", part)
		}

		if component == "all" {
			baseLevel = level
		} else {
			componentLevels = append(componentLevels, component+":"+level)
		}
	}

	return baseLevel, strings.Join(componentLevels, ","), nil
}
