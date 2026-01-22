// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/tetratelabs/envoy-ecosystem/cli/internal/extensions"
)

var (
	//go:embed config.yaml
	defaultConfig string

	// configTemplate is the parsed template of the default Envoy config.
	defaultConfigTemplate = template.Must(template.New("envoy-config").Parse(defaultConfig))
)

// ConfigGenerationParams holds parameters for generating the Envoy config.
type ConfigGenerationParams struct {
	// AdminPort is the port for Envoy admin interface.
	AdminPort int
	// ListenerPort is the port where Envoy listens for incoming traffic.
	ListenerPort int
	// Extensions to generate the config for
	Extensions []*extensions.Manifest
}

// RenderConfig renders the Envoy configuration with the given parameters.
// The ouyput is a YAML string that is passed to func-e to run Envoy.
func RenderConfig(params ConfigGenerationParams) (string, error) {
	filters := make([]any, 0, len(params.Extensions))
	for _, ext := range params.Extensions {
		filterConfig, err := generateFilterConfig(ext, nil)
		if err != nil {
			return "", fmt.Errorf("failed to generate filter config for extension %q: %w", ext.Name, err)
		}
		filters = append(filters, filterConfig)
	}

	_ = filters // TODO(nacx): remove when the variable is used (it's here to make linter happy)

	// TODO(nacx): include the filters in the configuration.
	//             we may awnt to change config generation from a Go template
	// 		        to something more structured (e.g., using go-control-plane types).

	var renderedConfig strings.Builder
	if err := defaultConfigTemplate.Execute(&renderedConfig, params); err != nil {
		return "", fmt.Errorf("failed to render Envoy config template: %w", err)
	}
	return renderedConfig.String(), nil
}
