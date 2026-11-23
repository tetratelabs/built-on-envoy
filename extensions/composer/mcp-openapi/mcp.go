// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import "strings"

// formatToolName derives a tool name when operationId is absent.
// Example: GET /v1/forecast → get_v1_forecast
func formatToolName(method, path string) string {
	s := strings.ToLower(method) + "_" + path
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)

	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' })
	return strings.Join(parts, "_")
}

// descriptionOf picks the best description for an MCP tool.
// Priority: x-mcp-description > description > summary > fallback.
func descriptionOf(extensions map[string]any, description, summary, fallback string) string {
	if s, ok := extensions["x-mcp-description"].(string); ok && s != "" {
		return s
	}
	if description != "" {
		return description
	}
	if summary != "" {
		return summary
	}
	return "Calls " + fallback
}
