// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// gen-manifest-reference generates MDX documentation for the extension manifest
// based on the JSON schema definition.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// JSONSchema represents a JSON Schema document.
type JSONSchema struct {
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Type        string               `json:"type"`
	Required    []string             `json:"required"`
	Properties  map[string]*Property `json:"properties"`
	AllOf       []*ConditionalSchema `json:"allOf"`
}

// Property represents a property in the JSON schema.
type Property struct {
	Type        any                  `json:"type"`
	Description string               `json:"description"`
	MinLength   *int                 `json:"minLength"`
	MaxLength   *int                 `json:"maxLength"`
	MinItems    *int                 `json:"minItems"`
	Pattern     string               `json:"pattern"`
	Enum        []string             `json:"enum"`
	UniqueItems bool                 `json:"uniqueItems"`
	Items       *Items               `json:"items"`
	Properties  map[string]*Property `json:"properties"`
	Required    []string             `json:"required"`
	OneOf       []*OneOfItem         `json:"oneOf"`
}

// Items represents the items definition for array types.
type Items struct {
	Type       string               `json:"type"`
	Enum       []string             `json:"enum"`
	MinLength  *int                 `json:"minLength"`
	Pattern    string               `json:"pattern"`
	Properties map[string]*Property `json:"properties"`
	Required   []string             `json:"required"`
}

// OneOfItem represents a oneOf constraint item.
type OneOfItem struct {
	Required []string `json:"required"`
}

// ConditionalSchema represents conditional schema rules (if/then/else).
type ConditionalSchema struct {
	If   *ConditionIf   `json:"if"`
	Then *ConditionThen `json:"then"`
	Else *ConditionElse `json:"else"`
}

// ConditionIf represents the if condition.
type ConditionIf struct {
	Properties map[string]*ConditionProperty `json:"properties"`
	Required   []string                      `json:"required"`
	Not        *ConditionIf                  `json:"not"`
	AllOf      []*ConditionIf                `json:"allOf"`
}

// ConditionProperty represents a property condition.
type ConditionProperty struct {
	Const string `json:"const"`
}

// ConditionThen represents the then clause.
type ConditionThen struct {
	Required   []string       `json:"required"`
	Properties map[string]any `json:"properties"`
}

// ConditionElse represents the else clause.
type ConditionElse struct {
	Properties map[string]any `json:"properties"`
}

// Constraint represents a constraint with a name and value.
type Constraint struct {
	Name  string
	Value string
}

// DocProperty represents a property for documentation.
type DocProperty struct {
	Name          string
	Type          string
	Description   string
	Required      bool
	Constraints   []Constraint
	EnumValues    []string
	SubProperties []DocProperty
	OneOf         []string
}

// TemplateData holds the data for the template.
type TemplateData struct {
	Title       string
	Description string
	Properties  []DocProperty
}

func main() {
	var (
		schemaPath         string
		outputPath         string
		extensionIndexPath string
	)

	flag.StringVar(&schemaPath, "schema", "", "Path to the JSON schema file (required)")
	flag.StringVar(&outputPath, "output", "", "Output path for the MDX file (required)")
	flag.StringVar(&extensionIndexPath, "extension-index", "", "Output path for the extension index file (required)")
	flag.Parse()

	if schemaPath == "" || outputPath == "" || extensionIndexPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: gen-manifest-reference -schema <path> -output <path> -extension-index <path>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if err := generateExtensionIndex(extensionIndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate extension index: %v\n", err)
		os.Exit(1)
	}

	// Read and parse the schema
	schemaData, err := os.ReadFile(filepath.Clean(schemaPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read schema file: %v\n", err)
		os.Exit(1)
	}

	var schema JSONSchema
	if err = json.Unmarshal(schemaData, &schema); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse schema: %v\n", err)
		os.Exit(1)
	}

	// Build required fields set
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	// Convert schema properties to doc properties
	var properties []DocProperty
	for name, prop := range schema.Properties {
		docProp := convertProperty(name, prop, requiredSet[name], schema.AllOf)
		properties = append(properties, docProp)
	}

	// Sort properties alphabetically
	sort.Slice(properties, func(i, j int) bool {
		return properties[i].Name < properties[j].Name
	})

	// Prepare template data
	data := TemplateData{
		Title:       schema.Title,
		Description: schema.Description,
		Properties:  properties,
	}

	// Load and execute template
	titleCase := cases.Title(language.English)
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"title": titleCase.String,
	}).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse templates: %v\n", err)
		os.Exit(1)
	}

	// Ensure output directory exists
	if err = os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	// Write output file
	f, err := os.Create(filepath.Clean(outputPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output file: %v\n", err)
		os.Exit(1)
	}

	if err = tmpl.ExecuteTemplate(f, "manifest.mdx.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute template: %v\n", err)
		_ = f.Close()
		os.Exit(1)
	}
	_ = f.Close()

	fmt.Printf("Generated: %s\n", outputPath)
}

func generateExtensionIndex(path string) error {
	manifests := extensions.ManifestsForCatalog()
	slices.SortFunc(manifests, func(a, b *extensions.Manifest) int {
		return strings.Compare(a.Name, b.Name)
	})
	index, err := json.MarshalIndent(manifests, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, index, 0o600); err != nil {
		return err
	}
	fmt.Printf("Generated: %s\n", path)
	return nil
}

// convertProperty converts a schema property to a documentation property.
func convertProperty(name string, prop *Property, required bool, allOf []*ConditionalSchema) DocProperty {
	docProp := DocProperty{
		Name:        name,
		Description: prop.Description,
		Required:    required,
	}

	// Determine type
	docProp.Type = getTypeString(prop)

	// Build constraints
	var constraints []Constraint

	if prop.MinLength != nil {
		constraints = append(constraints, Constraint{Name: "Min length", Value: fmt.Sprintf("%d", *prop.MinLength)})
	}
	if prop.MaxLength != nil {
		constraints = append(constraints, Constraint{Name: "Max length", Value: fmt.Sprintf("%d", *prop.MaxLength)})
	}
	if prop.MinItems != nil {
		constraints = append(constraints, Constraint{Name: "Min items", Value: fmt.Sprintf("%d", *prop.MinItems)})
	}
	if prop.Pattern != "" {
		constraints = append(constraints, Constraint{Name: "Pattern", Value: fmt.Sprintf("`%s`", prop.Pattern)})
	}
	if prop.UniqueItems {
		constraints = append(constraints, Constraint{Name: "Unique items", Value: "Yes"})
	}

	// Handle enum values
	if len(prop.Enum) > 0 {
		docProp.EnumValues = prop.Enum
	}

	// Handle array item constraints
	if prop.Items != nil {
		if len(prop.Items.Enum) > 0 {
			docProp.EnumValues = prop.Items.Enum
		}
		if prop.Items.Pattern != "" {
			constraints = append(constraints, Constraint{Name: "Item pattern", Value: fmt.Sprintf("`%s`", prop.Items.Pattern)})
		}
		if prop.Items.MinLength != nil {
			constraints = append(constraints, Constraint{Name: "Item min length", Value: fmt.Sprintf("%d", *prop.Items.MinLength)})
		}
	}

	// Handle oneOf constraints
	if len(prop.OneOf) > 0 {
		var oneOfOptions []string
		for _, o := range prop.OneOf {
			if len(o.Required) > 0 {
				oneOfOptions = append(oneOfOptions, strings.Join(o.Required, ", "))
			}
		}
		docProp.OneOf = oneOfOptions
	}

	// Check for conditional requirements in allOf
	for _, cond := range allOf {
		if cond.If == nil || cond.Then == nil {
			continue
		}
		constraints = append(constraints, conditionalConstraints(name, cond)...)
	}

	docProp.Constraints = constraints

	// Handle nested object properties
	if prop.Properties != nil {
		nestedRequired := make(map[string]bool)
		for _, r := range prop.Required {
			nestedRequired[r] = true
		}

		var subProps []DocProperty
		for subName, subProp := range prop.Properties {
			subDocProp := convertProperty(subName, subProp, nestedRequired[subName], nil)
			subProps = append(subProps, subDocProp)
		}
		sort.Slice(subProps, func(i, j int) bool {
			return subProps[i].Name < subProps[j].Name
		})
		docProp.SubProperties = subProps
	}

	// Handle array of objects
	if prop.Items != nil && prop.Items.Properties != nil {
		nestedRequired := make(map[string]bool)
		for _, r := range prop.Items.Required {
			nestedRequired[r] = true
		}

		var subProps []DocProperty
		for subName, subProp := range prop.Items.Properties {
			subDocProp := convertProperty(subName, subProp, nestedRequired[subName], nil)
			subProps = append(subProps, subDocProp)
		}
		sort.Slice(subProps, func(i, j int) bool {
			return subProps[i].Name < subProps[j].Name
		})
		docProp.SubProperties = subProps
	}

	return docProp
}

// conditionalConstraints returns constraints for a property based on a conditional allOf rule.
func conditionalConstraints(name string, cond *ConditionalSchema) []Constraint {
	var constraints []Constraint

	// Check if this property is required by the "then" clause
	thenRequires := false
	for _, r := range cond.Then.Required {
		if r == name {
			thenRequires = true
			break
		}
	}

	// Check if this property is forbidden by the "then" clause (property set to false)
	thenForbids := false
	if cond.Then.Properties != nil {
		if v, ok := cond.Then.Properties[name]; ok {
			if b, ok := v.(bool); ok && !b {
				thenForbids = true
			}
		}
	}

	if !thenRequires && !thenForbids {
		return nil
	}

	condDesc := describeCondition(cond.If)
	if condDesc == "" {
		return nil
	}

	if thenRequires {
		constraints = append(constraints, Constraint{
			Name:  "Required when",
			Value: condDesc,
		})
	}
	if thenForbids {
		constraints = append(constraints, Constraint{
			Name:  "Forbidden when",
			Value: condDesc,
		})
	}

	return constraints
}

// describeCondition returns a human-readable description of an if condition.
func describeCondition(cif *ConditionIf) string {
	if cif == nil {
		return ""
	}

	// Handle "not" conditions: e.g. { "not": { "required": ["parent"] } }
	if cif.Not != nil {
		return describeNegatedCondition(cif.Not)
	}

	// Handle "allOf" compound conditions
	if len(cif.AllOf) > 0 {
		var parts []string
		for _, sub := range cif.AllOf {
			desc := describeCondition(sub)
			if desc != "" {
				parts = append(parts, desc)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, " and ")
	}

	// Handle "required" conditions: e.g. { "required": ["parent"] }
	if len(cif.Required) > 0 {
		parts := make([]string, len(cif.Required))
		for i, r := range cif.Required {
			parts[i] = fmt.Sprintf("`%s` is set", r)
		}
		return strings.Join(parts, " and ")
	}

	// Handle "properties" with "const": e.g. { "properties": { "type": { "const": "lua" } } }
	if len(cif.Properties) > 0 {
		var parts []string
		for prop, val := range cif.Properties {
			parts = append(parts, fmt.Sprintf("`%s` is `%s`", prop, val.Const))
		}
		sort.Strings(parts)
		return strings.Join(parts, " and ")
	}

	return ""
}

// describeNegatedCondition returns a human-readable description of a negated condition.
// Instead of "not(`parent` is set)", it produces "`parent` is not set".
func describeNegatedCondition(cif *ConditionIf) string {
	if cif == nil {
		return ""
	}

	// Negate "required" → "is not set"
	if len(cif.Required) > 0 {
		parts := make([]string, len(cif.Required))
		for i, r := range cif.Required {
			parts[i] = fmt.Sprintf("`%s` is not set", r)
		}
		return strings.Join(parts, " and ")
	}

	// Negate "properties.const" → "is not"
	if len(cif.Properties) > 0 {
		var parts []string
		for prop, val := range cif.Properties {
			parts = append(parts, fmt.Sprintf("`%s` is not `%s`", prop, val.Const))
		}
		sort.Strings(parts)
		return strings.Join(parts, " and ")
	}

	// Fallback: wrap the positive description
	inner := describeCondition(cif)
	if inner == "" {
		return ""
	}
	return "not(" + inner + ")"
}

// getTypeString returns a human-readable type string from a property.
func getTypeString(prop *Property) string {
	typeStr := "unknown"

	switch t := prop.Type.(type) {
	case string:
		typeStr = t
	case []any:
		// Handle array of types (e.g., ["string", "null"])
		var types []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				types = append(types, s)
			}
		}
		typeStr = strings.Join(types, " | ")
	}

	// Handle arrays
	if typeStr == "array" && prop.Items != nil {
		if prop.Items.Type == "object" {
			typeStr = "[]object"
		} else if prop.Items.Type != "" {
			typeStr = "[]" + prop.Items.Type
		}
	}

	return typeStr
}
