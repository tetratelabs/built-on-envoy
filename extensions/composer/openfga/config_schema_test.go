// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openfga

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga.default.svc:8080",
			"store_id": "01ABCDEF",
			"authorization_model_id": "model-123",
			"user": {"header": "x-user-id", "prefix": "user:"},
			"relation": {"value": "can_use"},
			"object": {"header": "x-ai-model", "prefix": "model:"},
			"fail_open": false,
			"dry_run": false,
			"timeout_ms": 5000,
			"deny_status": 403,
			"deny_body": "Forbidden",
			"consistency": "HIGHER_CONSISTENCY",
			"callout_headers": {"authorization": "Bearer token"},
			"metadata": {"namespace": "openfga.authz", "key": "decision"},
			"contextual_tuples": [
				{
					"user": {"header": "x-user-id", "prefix": "user:"},
					"relation": {"value": "member"},
					"object": {"header": "x-org-id", "prefix": "organization:"}
				}
			],
			"context": {"ip": {"header": "x-forwarded-for"}}
		}`)
	})
	t.Run("valid minimal config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga:8080",
			"store_id": "store1",
			"user": {"header": "x-user-id"},
			"relation": {"value": "reader"},
			"object": {"header": "x-resource"}
		}`)
	})
	t.Run("valid multi-rule config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga:8080",
			"store_id": "store1",
			"user": {"header": "x-user-id", "prefix": "user:"},
			"rules": [
				{
					"match": {"headers": {"x-ai-eg-model": "*"}},
					"relation": {"value": "can_use"},
					"object": {"header": "x-ai-eg-model", "prefix": "model:"}
				},
				{
					"relation": {"value": "can_access"},
					"object": {"header": "x-resource-id", "prefix": "resource:"}
				}
			]
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid deny_status too low", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga:8080",
			"store_id": "store1",
			"deny_status": 99
		}`)
	})
	t.Run("invalid consistency value", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga:8080",
			"store_id": "store1",
			"consistency": "LOW_LATENCY"
		}`)
	})
	t.Run("invalid contextual tuple missing required fields", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga:8080",
			"store_id": "store1",
			"contextual_tuples": [
				{
					"user": {"header": "x-user-id"}
				}
			]
		}`)
	})
	t.Run("invalid deny_status too high", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"cluster": "openfga",
			"openfga_host": "openfga:8080",
			"store_id": "store1",
			"deny_status": 600
		}`)
	})
}
