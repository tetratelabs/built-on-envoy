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
)

var (
	//go:embed config.yaml
	defaultConfig string

	// configTemplate is the parsed template of the default Envoy config.
	defaultConfigTemplate = template.Must(template.New("envoy-config").Parse(defaultConfig))
)

// ConfigTemplateParams holds parameters for templating the Envoy config.
type ConfigTemplateParams struct {
	// AdminPort is the port for Envoy admin interface.
	AdminPort int
	// ListenerPort is the port where Envoy listens for incoming traffic.
	ListenerPort int
}

// RenderConfig renders the Envoy configuration with the given parameters.
func RenderConfig(params ConfigTemplateParams) (string, error) {
	var renderedConfig strings.Builder
	if err := defaultConfigTemplate.Execute(&renderedConfig, params); err != nil {
		return "", fmt.Errorf("failed to render Envoy config template: %w", err)
	}
	return renderedConfig.String(), nil
}
