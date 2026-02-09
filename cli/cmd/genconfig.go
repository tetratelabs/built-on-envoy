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
	"os"

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
	Configs []string `name:"config" sep:"none" help:"Optional JSON config string for extensions. Applied in order to combined --extension and --local flags."`
	OCI     OCIFlags `embed:""`

	extensions []*extensions.Manifest `kong:"-"` // Internal field: loaded extension manifests
	output     io.Writer              `kong:"-"` // Internal field for testing
}

//go:embed genconfig_help.md
var genConfigHelp string

// Help provides detailed help for the config command.
func (c *GenConfig) Help() string { return genConfigHelp }

// Run executes the GenConfig command.
func (c *GenConfig) Run(ctx context.Context, dirs *xdg.Directories) error {
	out := c.output
	if out == nil {
		out = os.Stdout
	}

	downloader := &extensions.Downloader{
		Username: c.OCI.Username,
		Password: c.OCI.Password,
		Insecure: c.OCI.Insecure,
		Dirs:     dirs,
	}

	downloaded, err := downloadExtensions(ctx, c.OCI.Registry, downloader, c.Extensions)
	if err != nil {
		return err
	}

	c.extensions, err = loadLocalManifests(append(downloaded, c.Local...))
	if err != nil {
		return err
	}

	var renderer envoy.ConfigRenderer
	if c.Minimal {
		renderer = envoy.MinimalConfigRenderer
	} else {
		renderer = envoy.FullConfigRenderer
	}

	config, err := envoy.RenderConfig(envoy.ConfigGenerationParams{
		AdminPort:    c.AdminPort,
		ListenerPort: c.ListenPort,
		DataHome:     dirs.DataHome,
		Extensions:   c.extensions,
		Configs:      c.Configs,
	}, renderer)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, config)

	return nil
}
