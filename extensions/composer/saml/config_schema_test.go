// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"entity_id": "https://sp.example.com",
			"acs_path": "/saml/acs",
			"idp_metadata_xml": {"file": "/path/to/idp-metadata.xml"},
			"sp_cert_pem": {"file": "/path/to/cert.pem"},
			"sp_key_pem": {"file": "/path/to/key.pem"},
			"session": {
				"cookie_name": "_my_session",
				"duration": "4h",
				"cookie_domain": "example.com",
				"cookie_secure": true,
				"cookie_signing_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			},
			"bypass_paths": ["/health", "/ready"],
			"subject_header": "x-saml-subject",
			"attribute_headers": {"email": "x-user-email"},
			"sign_authn_requests": true,
			"allowed_clock_skew": "10s",
			"name_id_format": "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
			"default_redirect_path": "/dashboard",
			"slo_path": "/saml/slo",
			"metadata_path": "/saml/metadata"
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid sp cert without key", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"entity_id": "https://sp.example.com",
			"acs_path": "/saml/acs",
			"idp_metadata_xml": {"inline": "<xml/>"},
			"sp_cert_pem": {"file": "/path/to/cert.pem"}
		}`)
	})
}
