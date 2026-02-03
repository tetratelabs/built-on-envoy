// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"sigs.k8s.io/yaml"

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
	Configs    []string `name:"config" help:"Optional JSON config string for extensions. Applied in order to combined --extension and --local flags. Use empty string to skip."`
	OCI        OCIFlags `embed:""`

	extensions []*extensions.Manifest `kong:"-"` // Internal field: loaded extension manifests
	output     io.Writer              `kong:"-"` // Internal field for testing
}

//go:embed config_help.md
var configHelp string

// Help provides detailed help for the config command.
func (c *GenConfig) Help() string { return configHelp }

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

	var config string
	if c.Minimal {
		config, err = c.generateMinimalConfig(dirs.DataHome)
	} else {
		config, err = envoy.RenderConfig(envoy.ConfigGenerationParams{
			AdminPort:    c.AdminPort,
			ListenerPort: c.ListenPort,
			DataHome:     dirs.DataHome,
			Extensions:   c.extensions,
			Configs:      c.Configs,
		})
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, config)

	return nil
}

// generateMinimalConfig generates the Envoy configuration with only the extension-generated resources.
func (c *GenConfig) generateMinimalConfig(dataHome string) (string, error) {
	filters := make([]*hcmv3.HttpFilter, 0, len(c.extensions))
	clusters := make([]*clusterv3.Cluster, 0)
	for i, ext := range c.extensions {
		var cfg string
		if i < len(c.Configs) {
			cfg = c.Configs[i]
		}
		resources, err := envoy.GenerateFilterConfig(ext, dataHome, cfg)
		if err != nil {
			return "", fmt.Errorf("failed to generate filter config for extension %q: %w", ext.Name, err)
		}
		filters = append(filters, resources.HTTPFilters...)
		clusters = append(clusters, resources.Clusters...)
	}

	filterConfigs, err := protoListToAny(filters)
	if err != nil {
		return "", fmt.Errorf("failed to serialize filter configs: %w", err)
	}

	payload := map[string]any{
		"http_filters": filterConfigs,
	}
	clusterConfigs, err := protoListToAny(clusters)
	if err != nil {
		return "", fmt.Errorf("failed to serialize cluster configs: %w", err)
	}
	if len(clusterConfigs) > 0 {
		payload["clusters"] = clusterConfigs
	}
	cfgYaml, err := yaml.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	return string(cfgYaml), nil
}

// protoListToAny converts a list of proto messages to a list of interface{} by marshaling to JSON and unmarshaling back.
func protoListToAny[T proto.Message](items []T) ([]any, error) {
	marshaler := protojson.MarshalOptions{UseProtoNames: true}
	out := make([]any, 0, len(items))
	for _, item := range items {
		raw, err := marshaler.Marshal(item)
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}

	return out, nil
}
