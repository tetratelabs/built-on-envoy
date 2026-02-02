// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package cmd contains the CLI commands
package cmd

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// defaultLogLevel is the default Envoy component log level.
const defaultLogLevel = "error"

// Run is a command to run Envoy with extensions.
type Run struct {
	EnvoyVersion string   `help:"Envoy version to use (e.g., 1.31.0)" env:"ENVOY_VERSION"`
	LogLevel     string   `help:"Envoy component log level." default:"all:error"`
	RunID        string   `name:"run-id" env:"BOE_RUN_ID" help:"Run identifier for this invocation. Defaults to timestamp-based ID or $BOE_RUN_ID. Use '0' for Docker/Kubernetes."`
	ListenPort   uint32   `help:"Port for Envoy listener to accept incoming traffic." default:"10000"`
	AdminPort    uint32   `help:"Port for Envoy admin interface." default:"9901"`
	Extensions   []string `name:"extension" help:"Extensions to enable (in the format: \"name\" or \"name:version\")." sep:","`
	Local        []string `name:"local" help:"Path to a directory containing a local Extension to enable." type:"existingdir" sep:","`
	Registry     string   `name:"registry" env:"BOE_REGISTRY" help:"OCI registry URL to fetch the extension from." default:"${default_registry}"`
	Insecure     bool     `name:"insecure" env:"BOE_REGISTRY_INSECURE" help:"Allow fetching from an insecure (HTTP) registry." default:"false"`
	Username     string   `name:"username" env:"BOE_REGISTRY_USERNAME" help:"Username for the OCI registry."`
	Password     string   `name:"password" env:"BOE_REGISTRY_PASSWORD" help:"Password for the OCI registry." type:"password"`

	defaultLogLevel   string `kong:"-"` // Internal field: parsed defaut log level
	componentLogLevel string `kong:"-"` // Internal field: parsed component log levels
}

//go:embed run_help.md
var runHelp string

// Help provides detailed help for the run command.
func (r *Run) Help() string { return runHelp }

// BeforeApply is called by Kong before applying defaults to set computed default values.
func (r *Run) BeforeApply() error {
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
	return nil
}

// Run executes the run command
func (r *Run) Run(ctx context.Context, dirs *xdg.Directories) error {
	downloader := &extensions.Downloader{
		Username: r.Username,
		Password: r.Password,
		Insecure: r.Insecure,
		Dirs:     dirs,
	}

	downloaded, err := downloadExtensions(ctx, r.Registry, downloader, r.Extensions)
	if err != nil {
		return err
	}

	extensions, err := loadLocalManifests(append(downloaded, r.Local...))
	if err != nil {
		return err
	}

	runner := &envoy.Runner{
		EnvoyVersion:      r.EnvoyVersion,
		DefaultLogLevel:   r.defaultLogLevel,
		ComponentLogLevel: r.componentLogLevel,
		Dirs:              dirs,
		RunID:             r.RunID,
		ListenPort:        r.ListenPort,
		AdminPort:         r.AdminPort,
		Extensions:        extensions,
	}

	return runner.Run(ctx)
}

// downloadExtensions downloads the specified extensions using the provided downloader.
func downloadExtensions(ctx context.Context, registry string, downloader *extensions.Downloader, refs []string) ([]string, error) {
	downloaded := make([]string, 0, len(refs))
	for _, ext := range refs {
		name, tag := splitRef(ext)
		repository := extensions.RepositoryName(registry, name)
		downloadDir, _, err := downloader.Download(ctx, repository, tag, "")
		if err != nil {
			return nil, err
		}
		downloaded = append(downloaded, downloadDir)
	}
	return downloaded, nil
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

var errFailedToLoadLocalManifest = errors.New("failed to load local manifest")

// loadLocalManifests loads extension manifests from the specified local paths.
func loadLocalManifests(paths []string) ([]*extensions.Manifest, error) {
	manifests := make([]*extensions.Manifest, 0, len(paths))
	for _, path := range paths {
		manifest, err := extensions.LoadLocalManifest(path + "/manifest.yaml")
		if err != nil {
			return nil, fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

// extractTag extracts the tag from a full OCI reference.
func splitRef(ref string) (repo string, tag string) {
	if colonIdx := strings.LastIndex(ref, ":"); colonIdx != -1 {
		return ref[:colonIdx], ref[colonIdx+1:]
	}
	return ref, "latest"
}
