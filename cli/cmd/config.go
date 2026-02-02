// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// GenConfig is a command to generate Envoy configuration with specified extensions.
type GenConfig struct {
	OnlyFilters bool     `kong:"help='Generate configuration with only extension filters.'"`
	ListenPort  uint32   `help:"Port for Envoy listener to accept incoming traffic." default:"10000"`
	AdminPort   uint32   `help:"Port for Envoy admin interface." default:"9901"`
	Extensions  []string `name:"extension" help:"Extensions to enable (in the format: \"name\" or \"name:version\")." sep:","`
	Local       []string `name:"local" help:"Path to a directory containing a local Extension to enable." type:"existingdir" sep:","`
	Registry    string   `name:"registry" env:"BOE_REGISTRY" help:"OCI registry URL to fetch the extension from." default:"${default_registry}"`
	Insecure    bool     `name:"insecure" env:"BOE_REGISTRY_INSECURE" help:"Allow fetching from an insecure (HTTP) registry." default:"false"`
	Username    string   `name:"username" env:"BOE_REGISTRY_USERNAME" help:"Username for the OCI registry."`
	Password    string   `name:"password" env:"BOE_REGISTRY_PASSWORD" help:"Password for the OCI registry." type:"password"`

	extensions []*extensions.Manifest `kong:"-"` // Internal field: loaded extension manifests
	output     io.Writer              `kong:"-"` // Internal field for testing
}

// Help provides detailed help for the gen-config command.
func (c *GenConfig) Help() string {
	return strings.ReplaceAll(`The gen-config command generates Envoy configuration YAML for the specified extensions.
This is useful for inspecting the generated configuration, integrating with existing Envoy deployments,
or using with external Envoy management tools.

By default, it outputs a complete Envoy bootstrap configuration. Use the {BT}--only-filters{BT} flag
to generate just the HTTP filter chain configuration, which can be embedded into an existing
{BT}HttpConnectionManager{BT} configuration.`, "{BT}", "`")
}

// Run executes the GenConfig command.
func (c *GenConfig) Run(ctx context.Context, dirs *xdg.Directories) error {
	out := c.output
	if out == nil {
		out = os.Stdout
	}

	downloader := &extensions.Downloader{
		Username: c.Username,
		Password: c.Password,
		Insecure: c.Insecure,
		Dirs:     dirs,
	}

	downloaded, err := downloadExtensions(ctx, c.Registry, downloader, c.Extensions)
	if err != nil {
		return err
	}

	c.extensions, err = loadLocalManifests(append(downloaded, c.Local...))
	if err != nil {
		return err
	}

	var config string

	if c.OnlyFilters {
		config, err = c.generateFilterConfig()
	} else {
		config, err = envoy.RenderConfig(envoy.ConfigGenerationParams{
			AdminPort:    c.AdminPort,
			ListenerPort: c.ListenPort,
			Extensions:   c.extensions,
		})
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, config)

	return nil
}

// generateFilterConfig generates the Envoy configuration with only the extension filters.
func (c *GenConfig) generateFilterConfig() (string, error) {
	filters := make([]*hcmv3.HttpFilter, 0, len(c.extensions))
	for _, ext := range c.extensions {
		// TODO(nacx): support config
		resources, err := envoy.GenerateFilterConfig(ext, nil)
		if err != nil {
			return "", fmt.Errorf("failed to generate filter config for extension %q: %w", ext.Name, err)
		}
		filters = append(filters, resources.HTTPFilters...)
	}

	hcm := &hcmv3.HttpConnectionManager{
		HttpFilters: filters,
	}

	cfgYaml, err := envoy.ProtoToYaml(hcm)
	if err != nil {
		return "", fmt.Errorf("failed to convert config to YAML: %w", err)
	}

	return string(cfgYaml), nil
}
