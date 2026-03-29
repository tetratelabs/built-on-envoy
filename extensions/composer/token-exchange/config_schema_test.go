// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oauth2te

import (
	"testing"

	pkg "github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		pkg.AssertSchemaValid(t, "config.schema.json", `{
			"cluster": "keycloak",
			"token_exchange_url": "https://idp.example.com/token",
			"client_id": "my-client",
			"client_secret": "my-secret",
			"subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
			"resource": "https://api.example.com",
			"audience": "target-service",
			"scope": "openid profile",
			"requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
			"actor_token": "actor-token-value",
			"actor_token_type": "urn:ietf:params:oauth:token-type:access_token",
			"timeout_ms": 3000
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid actor token without type", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{
			"cluster": "keycloak",
			"token_exchange_url": "https://idp.example.com/token",
			"client_id": "my-client",
			"client_secret": "my-secret",
			"actor_token": "some-token"
		}`)
	})
}
