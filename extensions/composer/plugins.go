// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package composer provides built-in plugins for the composer binary.
package composer

import (
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/azure-content-safety/embedded"     // Azure Content Safety plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/bedrock-guardrails/embedded"       // Bedrock Guardrails plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/cedar/embedded"                    // Cedar authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/chat-completions-decoder/embedded" // Chat Completions Decoder plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/example/embedded"                  // Example built-in plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/file-server/embedded"              // File server plugin.
	// Go plugin to loader other composer plugins that be compiled into separate shared libraries.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin-loader"
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt/embedded"       // JWE decryption plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/opa/embedded"               // OPA authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/openapi-validator/embedded" // OpenAPI validator plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/saml/embedded"              // SAML SP plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/token-exchange/embedded"    // OAuth2 Token Exchange plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/embedded"               // WAF plugin.
)
