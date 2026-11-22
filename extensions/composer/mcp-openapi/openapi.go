// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

const (
	mimeJSON = "application/json"
	typeNull = "null"
)

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	// TODO: add outputSchema when the bridge supports structured content. As of Envoy 1.40
	// it always returns the upstream response as a single text content item:
	// https://github.com/envoyproxy/envoy/blob/v1.40.0/source/extensions/filters/http/mcp_json_rest_bridge/mcp_json_rest_bridge_filter.cc#L125
	Annotations *annotations `json:"annotations,omitempty"`

	// Internal fields for bridge config generation (not serialized).
	httpMethod   string
	pathTemplate string
	hasBody      bool
}

type annotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

// config is the JSON config passed to the filter via --config.
type config struct {
	Spec       specSource `json:"spec"`
	IncludeOps []string   `json:"include_ops,omitempty"`
	ExcludeOps []string   `json:"exclude_ops,omitempty"`
}

// loadOpenAPISpec loads an OpenAPI spec from bytes.
// OpenAPI 3.x are loaded directly, where Swagger 2.0 are up-converted
func loadOpenAPISpec(data []byte) (*openapi3.T, error) {
	// Detect Swagger 2.0 by trying to unmarshal into openapi2.T.
	var v2 openapi2.T
	if err := json.Unmarshal(data, &v2); err == nil && v2.Swagger == "2.0" {
		doc, err := openapi2conv.ToV3(&v2)
		if err != nil {
			return nil, fmt.Errorf("converting swagger 2.0 to openapi 3: %w", err)
		}
		if err := doc.Validate(context.Background(), openapi3.DisableExamplesValidation()); err != nil {
			return nil, fmt.Errorf("validating converted spec: %w", err)
		}
		return doc, nil
	}

	// OpenAPI 3.x path.
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	if err := doc.Validate(loader.Context, openapi3.DisableExamplesValidation()); err != nil {
		return nil, fmt.Errorf("validating spec: %w", err)
	}
	return doc, nil
}

// serverBasePath extracts the base path from the spec's servers list.
// Uses kin-openapi's Servers.BasePath which handles server variable defaults
// and templated URLs.
func serverBasePath(doc *openapi3.T) string {
	bp, err := doc.Servers.BasePath()
	if err != nil || bp == "/" {
		return ""
	}
	return strings.TrimSuffix(bp, "/")
}

// generateTools returns MCP tools for each spec operation, sorted for deterministic output.
func generateTools(doc *openapi3.T, cfg *config) ([]tool, error) {
	basePath := serverBasePath(doc)
	var tools []tool

	for path, pathItem := range doc.Paths.Map() {
		for method, op := range pathItem.Operations() {
			name := op.OperationID
			if name == "" {
				name = formatToolName(method, path)
			}

			if len(cfg.IncludeOps) > 0 && !slices.Contains(cfg.IncludeOps, name) {
				continue
			}
			if slices.Contains(cfg.ExcludeOps, name) {
				continue
			}

			t, err := buildTool(name, method, basePath+path, op, pathItem.Parameters)
			if err != nil {
				return nil, fmt.Errorf("building tool %s: %w", name, err)
			}
			tools = append(tools, t)
		}
	}

	slices.SortFunc(tools, func(a, b tool) int {
		return strings.Compare(a.Name, b.Name)
	})
	return tools, nil
}

func buildTool(name, method, fullPath string, op *openapi3.Operation, pathParams openapi3.Parameters) (tool, error) {
	schema := map[string]any{"type": "object"}
	properties := map[string]any{}
	var required []string
	hasBody := false

	// Path-level params are inherited; operation params override on name collision.
	params := append(openapi3.Parameters{}, pathParams...)
	params = append(params, op.Parameters...)

	for _, param := range params {
		if param.Value == nil {
			continue
		}
		m, err := schemaToMap(param.Value.Schema)
		if err != nil {
			return tool{}, fmt.Errorf("parameter %s: %w", param.Value.Name, err)
		}
		if param.Value.Description != "" {
			if _, ok := m["description"]; !ok {
				m["description"] = param.Value.Description
			}
		}
		properties[param.Value.Name] = m
		if param.Value.Required {
			required = append(required, param.Value.Name)
		}
	}

	if op.RequestBody != nil && op.RequestBody.Value != nil {
		if mt := op.RequestBody.Value.Content.Get(mimeJSON); mt != nil && mt.Schema != nil {
			hasBody = true
			if s := mt.Schema.Value; s != nil {
				for propName, prop := range s.Properties {
					if prop.Value != nil && prop.Value.ReadOnly {
						continue
					}
					m, err := schemaToMap(prop)
					if err != nil {
						return tool{}, fmt.Errorf("body property %s: %w", propName, err)
					}
					properties[propName] = m
				}
				for _, r := range s.Required {
					if p := s.Properties[r]; p != nil && p.Value != nil && p.Value.ReadOnly {
						continue
					}
					required = append(required, r)
				}
			}
		}
	}

	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	return tool{
		Name:         name,
		Description:  descriptionOf(op.Extensions, op.Description, op.Summary, name),
		InputSchema:  schema,
		Annotations:  annotationsForMethod(method),
		httpMethod:   method,
		pathTemplate: fullPath,
		hasBody:      hasBody,
	}, nil
}

// schemaToMap round-trips through JSON so tool schemas are standalone fragments
// instead of OpenAPI $ref stubs, then cleans OpenAPI-only keywords.
func schemaToMap(ref *openapi3.SchemaRef) (map[string]any, error) {
	if ref == nil || ref.Value == nil {
		return nil, nil
	}
	b, err := json.Marshal(ref.Value)
	if err != nil {
		return nil, fmt.Errorf("marshaling schema: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling schema: %w", err)
	}
	return cleanForMCPInputSchema(m), nil
}

// cleanForMCPInputSchema converts an OpenAPI Schema (as map) into clean
// JSON Schema suitable for MCP tool inputSchema.
func cleanForMCPInputSchema(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	for _, k := range []string{
		"discriminator", "xml", "externalDocs", "example",
		"deprecated", "writeOnly", "readOnly",
	} {
		delete(m, k)
	}

	if nullable, ok := m["nullable"].(bool); ok && nullable {
		delete(m, "nullable")

		switch t := m["type"].(type) {
		case string:
			if t != typeNull {
				m["type"] = []any{t, typeNull}
			}
		case []any:
			hasNull := false
			for _, v := range t {
				if v == typeNull {
					hasNull = true
					break
				}
			}
			if !hasNull {
				m["type"] = append(t, typeNull)
			}
		default:
			m["type"] = []any{typeNull}
		}
	}

	if props, ok := m["properties"].(map[string]any); ok {
		cleanProps := make(map[string]any, len(props))
		for name, p := range props {
			pm, ok := p.(map[string]any)
			if !ok {
				cleanProps[name] = p
				continue
			}
			if ro, _ := pm["readOnly"].(bool); ro { //nolint:errcheck // type assert to zero value
				continue
			}
			cleanProps[name] = cleanForMCPInputSchema(pm)
		}
		if len(cleanProps) == 0 {
			delete(m, "properties")
		} else {
			m["properties"] = cleanProps
		}

		// Remove readOnly property names from required.
		if req, ok := m["required"].([]any); ok {
			var cleaned []any
			for _, r := range req {
				s, _ := r.(string) //nolint:errcheck // type assert to zero value
				if s != "" {
					if _, exists := cleanProps[s]; !exists {
						continue
					}
				}
				cleaned = append(cleaned, r)
			}
			if len(cleaned) == 0 {
				delete(m, "required")
			} else {
				m["required"] = cleaned
			}
		}
	}

	for _, key := range []string{"items", "additionalProperties", "not"} {
		if sub, ok := m[key].(map[string]any); ok {
			m[key] = cleanForMCPInputSchema(sub)
		}
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		if arr, ok := m[key].([]any); ok {
			for i, item := range arr {
				if sub, ok := item.(map[string]any); ok {
					arr[i] = cleanForMCPInputSchema(sub)
				}
			}
		}
	}

	return m
}

func annotationsForMethod(method string) *annotations {
	yes, no := true, false
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return &annotations{ReadOnlyHint: &yes, DestructiveHint: &no, IdempotentHint: &yes}
	case http.MethodPut:
		return &annotations{DestructiveHint: &no, IdempotentHint: &yes}
	case http.MethodDelete:
		return &annotations{DestructiveHint: &yes, IdempotentHint: &yes}
	default: // POST, PATCH
		return &annotations{}
	}
}
