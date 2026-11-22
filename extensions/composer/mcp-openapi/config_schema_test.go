// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"spec": {"url": "https://petstore.swagger.io/v2/swagger.json"},
			"include_ops": ["getPetById"],
			"exclude_ops": ["deletePet"]
		}`)
	})
	t.Run("valid file spec", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{"spec": {"file": "/path/to/api.yaml"}}`)
	})
	t.Run("valid inline spec", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{"spec": {"inline": "openapi: 3.0.3"}}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("empty spec", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"spec": {}}`)
	})
	t.Run("string spec", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"spec": "api.yaml"}`)
	})
	t.Run("multiple spec sources", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"spec": {"file": "api.yaml", "url": "https://example.com/api.yaml"}}`)
	})
	t.Run("non http url", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"spec": {"url": "ftp://example.com/api.yaml"}}`)
	})
	t.Run("unknown field", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"spec": {"file": "api.yaml"}, "foo": "bar"}`)
	})
}
