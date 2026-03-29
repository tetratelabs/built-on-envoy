// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cedar

import (
	"testing"

	pkg "github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		pkg.AssertSchemaValid(t, "config.schema.json", `{
			"policy": {"file": "/path/to/policy.cedar"},
			"entities_file": "/path/to/entities.json",
			"principal_type": "User",
			"principal_id_header": "x-user-id",
			"action_type": "Action",
			"resource_type": "Resource",
			"deny_status": 403,
			"deny_body": "Forbidden",
			"deny_headers": {"x-denied": "true"},
			"fail_open": false,
			"dry_run": false
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid deny status", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{
			"policy": {"inline": "permit(principal, action, resource);"},
			"principal_type": "User",
			"principal_id_header": "x-user-id",
			"deny_status": 99
		}`)
	})
}
