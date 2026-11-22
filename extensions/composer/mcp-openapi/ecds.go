// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

//go:embed tools_list.lua
var luaScriptTemplate string

const (
	luaPlaceholder = "__TOOLS_LIST_JSON__"

	typedExtensionConfigType = "type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"
	bridgeFilterName         = "envoy.filters.http.mcp_json_rest_bridge"
	bridgeConfigType         = "type.googleapis.com/envoy.extensions.filters.http.mcp_json_rest_bridge.v3.McpJsonRestBridge"
	luaFilterName            = "envoy.filters.http.lua"
	luaConfigType            = "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua"
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
}

type bridgeTool struct {
	Name     string         `json:"name"`
	HTTPRule bridgeHTTPRule `json:"http_rule"`
}

type bridgeHTTPRule struct {
	Get    string `json:"get,omitempty"`
	Post   string `json:"post,omitempty"`
	Put    string `json:"put,omitempty"`
	Delete string `json:"delete,omitempty"`
	Patch  string `json:"patch,omitempty"`
	Body   string `json:"body,omitempty"`
}

type luaECDSConfig struct {
	VersionInfo string              `json:"version_info"`
	Resources   []luaTypedExtension `json:"resources"`
}

type luaTypedExtension struct {
	Type        string         `json:"@type"`
	Name        string         `json:"name"`
	TypedConfig luaTypedConfig `json:"typed_config"`
}

type luaTypedConfig struct {
	Type              string        `json:"@type"`
	DefaultSourceCode luaSourceCode `json:"default_source_code"`
}

type luaSourceCode struct {
	InlineString string `json:"inline_string"`
}

// writeECDSConfig writes the MCP-to-REST bridge ECDS configuration for Envoy.
func writeECDSConfig(root *os.Root, name string, tools []tool) error {
	bt := make([]bridgeTool, len(tools))
	for i := range tools {
		bt[i] = buildBridgeTool(&tools[i])
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
	return root.WriteFile(name, data, 0o600)
}

// writeLuaECDSConfig writes the Lua ECDS configuration that answers tools/list
// locally. See tools_list.lua for why the bridge is not used for this.
func writeLuaECDSConfig(root *os.Root, name string, tools []tool) error {
	toolsListJSON, err := json.Marshal(toolsListResult{Tools: tools})
	if err != nil {
		return fmt.Errorf("marshaling tools list: %w", err)
	}
	script := strings.Replace(luaScriptTemplate, luaPlaceholder, string(toolsListJSON), 1)

	cfg := luaECDSConfig{
		VersionInfo: "1",
		Resources: []luaTypedExtension{{
			Type: typedExtensionConfigType,
			Name: luaFilterName,
			TypedConfig: luaTypedConfig{
				Type:              luaConfigType,
				DefaultSourceCode: luaSourceCode{InlineString: script},
			},
		}},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling Lua ECDS config: %w", err)
	}
	return root.WriteFile(name, data, 0o600)
}

// buildBridgeTool converts an internal tool to the bridge's HTTP rule format.
func buildBridgeTool(t *tool) bridgeTool {
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
	return bridgeTool{Name: t.Name, HTTPRule: rule}
}
