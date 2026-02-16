// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// GenConfig is a command to generate Envoy configuration with specified extensions.
type GenConfig struct {
	Minimal    bool     `kong:"help='Generate configuration with only extension-generated resources (HTTP filters and clusters).'"`
	ListenPort uint32   `help:"Port for Envoy listener to accept incoming traffic." default:"10000"`
	AdminPort  uint32   `help:"Port for Envoy admin interface." default:"9901"`
	Extensions []string `name:"extension" help:"Extensions to enable (in the format: \"name\" or \"name:version\")." sep:","`
	Local      []string `name:"local" help:"Path to a directory containing a local Extension to enable." type:"existingdir" sep:","`
	// sep:"none" disables Kong's default comma-separated splitting for []string flags.
	// JSON config values contain commas (e.g. {"a":"1","b":"2"}) which would otherwise
	// be split into separate invalid fragments, causing protobuf unmarshal failures.
	Configs  []string `name:"config" sep:"none" help:"Optional JSON config string for extensions. Applied in order to combined --extension and --local flags."`
	Clusters []string `name:"cluster" sep:"none" help:"Optional additional Envoy cluster. Supports JSON or short format (host:tlsPort)."`
	OCI      OCIFlags `embed:""`

	extensionPositions extensionPositions `kong:"-"` // Internal field: tracks the original position of extensions specified via both --extension and --local flags
	output             io.Writer          `kong:"-"` // Internal field for testing
}

//go:embed genconfig_help.md
var genConfigHelp string

// Help provides detailed help for the config command.
func (g *GenConfig) Help() string { return genConfigHelp }

// BeforeResolve is called by Kong before resolving the command to save the positions of extensions specified
// via --extension and --local flags, to ensure they are considered in the expected order.
func (g *GenConfig) BeforeResolve() error {
	var err error
	g.extensionPositions, err = saveExtensionPositions(os.Args)
	return err
}

// Run executes the GenConfig command.
func (g *GenConfig) Run(ctx context.Context, dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling genconfig command", "cmd", internal.RedactSensitive(g))

	out := g.output
	if out == nil {
		out = os.Stdout
	}

	downloader := &extensions.Downloader{
		Logger:   logger,
		Registry: g.OCI.Registry,
		Username: g.OCI.Username,
		Password: g.OCI.Password,
		Insecure: g.OCI.Insecure,
		Dirs:     dirs,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}

	downloaded, err := downloadExtensions(ctx, downloader, g.Extensions, false)
	if err != nil {
		return err
	}
	local, err := loadLocalManifests(ctx, logger, downloader, g.Local, false)
	if err != nil {
		return err
	}
	extensions, err := g.extensionPositions.sort(append(downloaded, local...))
	if err != nil {
		return err
	}

	var renderer envoy.ConfigRenderer
	if g.Minimal {
		renderer = envoy.MinimalConfigRenderer
	} else {
		renderer = envoy.FullConfigRenderer
	}

	config, err := envoy.RenderConfig(&envoy.ConfigGenerationParams{
		Logger:       logger,
		AdminPort:    g.AdminPort,
		ListenerPort: g.ListenPort,
		Dirs:         dirs,
		Extensions:   extensions,
		Configs:      g.Configs,
		Clusters:     g.Clusters,
	}, renderer)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, config)

	return nil
}
