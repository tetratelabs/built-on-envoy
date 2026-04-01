// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openapivalidator

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"spec": {"file": "/path/to/openapi.yaml"},
			"max_body_bytes": 1048576,
			"allow_unmatched_paths": true,
			"dry_run": false,
			"deny_response": {
				"status": 400,
				"body": "Bad Request",
				"headers": {"x-error": "validation-failed"}
			}
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid deny response status", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"spec": {"inline": "openapi: 3.0.0"},
			"deny_response": {"status": 99}
		}`)
	})
}
