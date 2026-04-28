// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package opa

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"policies": [
				{"inline": "package envoy.authz\ndefault allow = false"},
				{"file": "/path/to/policy.rego"}
			],
			"decision_path": "envoy.authz.allow",
			"fail_open": false,
			"dry_run": true,
			"with_body": true,
			"metadata_namespaces": ["envoy.filters.http.jwt_authn"]
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid empty policies", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"policies": []
		}`)
	})
}
