// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	pkg "github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		pkg.AssertSchemaValid(t, "config.schema.json", `{
			"private_key": {"file": "/path/to/key.pem"},
			"algorithm": "RSA-OAEP",
			"input_header": "Authorization",
			"prefix": "Bearer ",
			"output_header": "x-jwt-payload",
			"output_metadata": {"namespace": "jwe-decrypt", "key": "payload"}
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid private key both set", func(t *testing.T) {
		pkg.AssertSchemaInvalid(t, "config.schema.json", `{
			"private_key": {"inline": "key-data", "file": "/path/to/key.pem"},
			"algorithm": "RSA-OAEP"
		}`)
	})
}
