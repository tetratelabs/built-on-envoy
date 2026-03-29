// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"testing"

	pkg "github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		pkg.AssertSchemaValid(t, "config.schema.json", `{
			"directives": ["SecRuleEngine On", "Include @recommended.conf"],
			"mode": "FULL"
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid mode", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{
			"directives": ["SecRuleEngine On"],
			"mode": "INVALID"
		}`)
	})
}
