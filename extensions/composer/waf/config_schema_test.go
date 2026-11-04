// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"directives": ["SecRuleEngine On", "Include @recommended.conf"],
			"mode": "FULL",
			"request_uri_path_only": true
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("empty directives array", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"directives": []
		}`)
	})
	t.Run("empty directive string", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"directives": [""]
		}`)
	})
	t.Run("invalid mode", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"directives": ["SecRuleEngine On"],
			"mode": "INVALID"
		}`)
	})
	t.Run("metrics authority settings", func(t *testing.T) {
		for _, metrics := range []string{
			`{}`,
			`{"authority_label": "authority"}`,
			`{"authority_label": "route_name"}`,
			`{"authority_label": "virtual_host_name"}`,
			`{"authority_label": "none"}`,
		} {
			internaltesting.AssertSchemaValid(t, "config.schema.json", `{
				"directives": ["SecRuleEngine On"],
				"metrics": `+metrics+`
			}`)
		}
	})
	t.Run("invalid metrics", func(t *testing.T) {
		for _, metrics := range []string{
			`true`,
			`{"authority_label": false}`,
			`{"authority_label": "host"}`,
			`{"authority_lable": false}`,
			`{"authority_literal": "shop"}`,
			`{"label_source": "route_name"}`,
		} {
			internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
				"directives": ["SecRuleEngine On"],
				"metrics": `+metrics+`
			}`)
		}
	})
}
