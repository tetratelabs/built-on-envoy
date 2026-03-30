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
			"bedrock_endpoint": "https://bedrock-runtime.us-east-1.amazonaws.com",
			"bedrock_cluster": "bedrock",
			"bedrock_api_key": "my-api-key",
			"bedrock_guardrails": [
				{"identifier": "guard-1", "version": "1"},
				{"identifier": "guard-2", "version": "2"}
			],
			"bedrock_timeoutms": 5000
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid empty guardrails", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"bedrock_endpoint": "https://example.com",
			"bedrock_cluster": "bedrock",
			"bedrock_api_key": "key",
			"bedrock_guardrails": []
		}`)
	})
}
