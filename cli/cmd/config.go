// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"fmt"
	"io"
	"os"

	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

// GenConfig is a command to generate Envoy configuration with specified extensions.
type GenConfig struct {
	OnlyFilters bool `kong:"help='Generate configuration with only extension filters.'"`

	ListenPort uint32   `help:"Port for Envoy listener to accept incoming traffic (default: 10000)" default:"10000"`
	AdminPort  uint32   `help:"Port for Envoy admin interface (default: 9901)" default:"9901"`
	Extensions []string `name:"extension" help:"Extensions to enable (by name)." sep:","`
	Local      []string `name:"local" help:"Path to a directory containing a local Extension to enable." type:"existingdir" sep:","`

	extensions []*extensions.Manifest `kong:"-"` // Internal field: loaded extension manifests
	output     io.Writer              `kong:"-"` // Internal field for testing
}

// Validate is called by Kong after parsing to validate the command arguments.
func (c *GenConfig) Validate() error {
	var err error
	c.extensions, err = validateExtensions(c.Extensions, c.Local)
	return err
}

// Run executes the GenConfig command.
func (c *GenConfig) Run() error {
	out := c.output
	if out == nil {
		out = os.Stdout
	}

	var (
		config string
		err    error
	)
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
		filterConfig, err := envoy.GenerateFilterConfig(ext, nil)
		if err != nil {
			return "", fmt.Errorf("failed to generate filter config for extension %q: %w", ext.Name, err)
		}
		filters = append(filters, filterConfig)
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
