// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"endpoint": "https://my-resource.cognitiveservices.azure.com",
			"api_key": {"inline": "my-api-key"},
			"mode": "monitor",
			"fail_open": true,
			"api_version": "2024-09-01",
			"hate_threshold": 4,
			"self_harm_threshold": 4,
			"sexual_threshold": 4,
			"violence_threshold": 4,
			"categories": ["Hate", "Violence"],
			"enable_protected_material": true,
			"enable_task_adherence": true,
			"task_adherence_api_version": "2025-09-15-preview"
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid threshold out of range", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"endpoint": "https://example.com",
			"api_key": {"inline": "key"},
			"hate_threshold": 10
		}`)
	})
}
