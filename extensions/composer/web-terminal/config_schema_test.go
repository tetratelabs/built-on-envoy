// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("empty config is valid (defaults apply)", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{}`)
	})
	t.Run("full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"command": "/bin/bash",
			"args": ["-l"],
			"writable": false,
			"serve_frontend": true
		}`)
	})
	t.Run("empty command is invalid", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"command": ""}`)
	})
	t.Run("unknown property is invalid", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{"nope": true}`)
	})
}
