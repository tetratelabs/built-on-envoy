// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"embed"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/*
var testdata embed.FS

func TestLoadOpenAPISpec(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expectedErr string
	}{
		{"swagger 2.0", readTestdata(t, "testdata/swagger2.json"), ""},
		{"openapi 3.x", readTestdata(t, "testdata/test_api.yml"), ""},
		{"invalid", []byte(`not valid`), "parsing spec: failed to unmarshal data: json error: jsontext: invalid character 'o' in literal null (expecting 'u') after offset 1, yaml error: yaml: construct errors: line 1: cannot construct !!str `not valid` into openapi3.T"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := loadOpenAPISpec(tt.data)
			if tt.expectedErr != "" {
				require.EqualError(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, doc.Paths)
		})
	}
}

func TestServerBasePath(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		expected string
	}{
		{"swagger 2.0 basePath", string(readTestdata(t, "testdata/swagger2.json")), "/v2"},
		{"server url path", `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"servers":[{"url":"https://example.com/api/v1/"}],"paths":{}}`, "/api/v1"},
		{"server url variables", `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"servers":[{"url":"https://example.com/{base}","variables":{"base":{"default":"v3"}}}],"paths":{}}`, "/v3"},
		{"server url without path", `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"servers":[{"url":"https://example.com"}],"paths":{}}`, ""},
		{"no servers", `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := loadOpenAPISpec([]byte(tt.spec))
			require.NoError(t, err)
			require.Equal(t, tt.expected, serverBasePath(doc))
		})
	}
}

func TestGenerateToolsBasePath(t *testing.T) {
	doc := loadTestSpec(t, "testdata/swagger2.json")
	tools, err := generateTools(doc, &config{})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "/v2/ping", tools[0].pathTemplate)
}

func TestGenerateTools(t *testing.T) {
	yes, no := true, false
	doc := loadTestSpec(t, "testdata/test_api.yml")

	allTools := []tool{
		{Name: "createItem", Description: "Create an item", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"tag":  map[string]any{"type": []any{"string", "null"}},
			},
			"required": []string{"name"},
		}, Annotations: &annotations{}, httpMethod: "POST", pathTemplate: "/items", hasBody: true},
		{Name: "deleteItem", Description: "Delete an item", InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "integer", "format": "int64", "description": "The item ID"}},
			"required":   []string{"id"},
		}, Annotations: &annotations{DestructiveHint: &yes, IdempotentHint: &yes}, httpMethod: "DELETE", pathTemplate: "/items/{id}", hasBody: false},
		{Name: "getItem", Description: "Get an item", InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "integer", "format": "int64", "description": "The item ID"}},
			"required":   []string{"id"},
		}, Annotations: &annotations{ReadOnlyHint: &yes, DestructiveHint: &no, IdempotentHint: &yes}, httpMethod: "GET", pathTemplate: "/items/{id}", hasBody: false},
		{Name: "get_search", Description: "Search items", InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"q": map[string]any{"type": "string", "description": "Search query"}},
			"required":   []string{"q"},
		}, Annotations: &annotations{ReadOnlyHint: &yes, DestructiveHint: &no, IdempotentHint: &yes}, httpMethod: "GET", pathTemplate: "/search", hasBody: false},
	}

	tests := []struct {
		name     string
		cfg      config
		expected []tool
	}{
		{"all operations", config{}, allTools},
		{"include filter", config{IncludeOps: []string{"getItem", "createItem"}}, []tool{allTools[0], allTools[2]}},
		{"exclude filter", config{ExcludeOps: []string{"deleteItem"}}, []tool{allTools[0], allTools[2], allTools[3]}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, err := generateTools(doc, &tt.cfg)
			require.NoError(t, err)
			require.Equal(t, tt.expected, tools)
		})
	}
}

func TestSchemaToMap(t *testing.T) {
	tests := []struct {
		name     string
		ref      *openapi3.SchemaRef
		expected map[string]any
	}{
		{"nil ref", nil, nil},
		{"nil value", &openapi3.SchemaRef{}, nil},
		{"integer with format", openapi3.NewSchemaRef("", &openapi3.Schema{
			Type:        &openapi3.Types{"integer"},
			Format:      "int64",
			Description: "ID",
		}), map[string]any{"type": "integer", "format": "int64", "description": "ID"}},
		{"nullable string", openapi3.NewSchemaRef("", &openapi3.Schema{
			Type:     &openapi3.Types{"string"},
			Nullable: true,
		}), map[string]any{"type": []any{"string", "null"}}},
		{"readOnly stripped", openapi3.NewSchemaRef("", &openapi3.Schema{
			Type:     &openapi3.Types{"string"},
			ReadOnly: true,
		}), map[string]any{"type": "string"}},
		{"noise fields stripped", openapi3.NewSchemaRef("", &openapi3.Schema{
			Type:       &openapi3.Types{"string"},
			Deprecated: true,
			Extensions: map[string]any{"example": "foo"},
		}), map[string]any{"type": "string"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := schemaToMap(tt.ref)
			require.NoError(t, err)
			require.Equal(t, tt.expected, m)
		})
	}
}

func TestAnnotationsForMethod(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name     string
		method   string
		expected *annotations
	}{
		{"read-only idempotent", "GET", &annotations{ReadOnlyHint: &yes, DestructiveHint: &no, IdempotentHint: &yes}},
		{"HEAD same as GET", "HEAD", &annotations{ReadOnlyHint: &yes, DestructiveHint: &no, IdempotentHint: &yes}},
		{"OPTIONS same as GET", "OPTIONS", &annotations{ReadOnlyHint: &yes, DestructiveHint: &no, IdempotentHint: &yes}},
		{"idempotent non-destructive", "PUT", &annotations{DestructiveHint: &no, IdempotentHint: &yes}},
		{"destructive idempotent", "DELETE", &annotations{DestructiveHint: &yes, IdempotentHint: &yes}},
		{"no hints", "POST", &annotations{}},
		{"PATCH same as POST", "PATCH", &annotations{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, annotationsForMethod(tt.method))
		})
	}
}

func readTestdata(t *testing.T, path string) []byte {
	t.Helper()
	data, err := testdata.ReadFile(path)
	require.NoError(t, err)
	return data
}

func loadTestSpec(t *testing.T, path string) *openapi3.T {
	t.Helper()
	doc, err := loadOpenAPISpec(readTestdata(t, path))
	require.NoError(t, err)
	return doc
}
