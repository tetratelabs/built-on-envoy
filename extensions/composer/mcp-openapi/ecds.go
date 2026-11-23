// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const (
	typedExtensionConfigType = "type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"
	bridgeFilterName         = "envoy.filters.http.mcp_json_rest_bridge"
	bridgeConfigType         = "type.googleapis.com/envoy.extensions.filters.http.mcp_json_rest_bridge.v3.McpJsonRestBridge"
)

type bridgeECDSConfig struct {
	VersionInfo string                 `json:"version_info"`
	Resources   []bridgeTypedExtension `json:"resources"`
}

type bridgeTypedExtension struct {
	Type        string            `json:"@type"`
	Name        string            `json:"name"`
	TypedConfig bridgeTypedConfig `json:"typed_config"`
}

type bridgeTypedConfig struct {
	Type       string          `json:"@type"`
	ToolConfig bridgeToolGroup `json:"tool_config"`
}

type bridgeToolGroup struct {
	Tools []bridgeTool `json:"tools"`
	// ToolListLocal makes the bridge answer tools/list locally, using each tool's ToolListConfig.
	ToolListLocal struct{} `json:"tool_list_local"`
}

type bridgeTool struct {
	Name           string               `json:"name"`
	HTTPRule       bridgeHTTPRule       `json:"http_rule"`
	ToolListConfig bridgeToolListConfig `json:"tool_list_config"`
}

type bridgeHTTPRule struct {
	Get    string `json:"get,omitempty"`
	Post   string `json:"post,omitempty"`
	Put    string `json:"put,omitempty"`
	Delete string `json:"delete,omitempty"`
	Patch  string `json:"patch,omitempty"`
	Body   string `json:"body,omitempty"`
}

type bridgeToolListConfig struct {
	Description string `json:"description"`
	// InputSchema is the tool's input JSON Schema, serialized as a string.
	InputSchema string `json:"input_schema"`
}

// writeECDSConfig writes the MCP-to-REST bridge ECDS configuration for Envoy to the given path.
func writeECDSConfig(path string, tools []tool) error {
	bt := make([]bridgeTool, len(tools))
	for i := range tools {
		t, err := buildBridgeTool(&tools[i])
		if err != nil {
			return err
		}
		bt[i] = t
	}

	cfg := bridgeECDSConfig{
		VersionInfo: "1",
		Resources: []bridgeTypedExtension{{
			Type: typedExtensionConfigType,
			Name: bridgeFilterName,
			TypedConfig: bridgeTypedConfig{
				Type:       bridgeConfigType,
				ToolConfig: bridgeToolGroup{Tools: bt},
			},
		}},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling ECDS config: %w", err)
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic writes data to a temporary file in the same directory as path, and then
// renames it to path. Envoy only picks up changes to file-based config sources that are moved
// into place, and this also prevents Envoy from reading a partially written file.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// buildBridgeTool converts an internal tool to the bridge's tool configuration.
func buildBridgeTool(t *tool) (bridgeTool, error) {
	rule := bridgeHTTPRule{}
	switch t.httpMethod {
	case http.MethodGet:
		rule.Get = t.pathTemplate
	case http.MethodPost:
		rule.Post = t.pathTemplate
	case http.MethodPut:
		rule.Put = t.pathTemplate
	case http.MethodDelete:
		rule.Delete = t.pathTemplate
	case http.MethodPatch:
		rule.Patch = t.pathTemplate
	}
	if t.hasBody {
		rule.Body = "*"
	}

	schema, err := json.Marshal(t.InputSchema)
	if err != nil {
		return bridgeTool{}, fmt.Errorf("marshaling input schema for tool %s: %w", t.Name, err)
	}
	return bridgeTool{
		Name:     t.Name,
		HTTPRule: rule,
		ToolListConfig: bridgeToolListConfig{
			Description: t.Description,
			InputSchema: string(schema),
		},
	}, nil
}
