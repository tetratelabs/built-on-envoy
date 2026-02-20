// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package gobundle provides built-in plugins for the gobundle binary.
package gobundle

import (
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/cedar/embedded"   // Cedar authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/example/embedded" // Example built-in plugin.

	// Go plugin to loader other gobundle plugins that be compiled into separate shared libraries.
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/goplugin"
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/jwe-decrypt/embedded"       // JWE decryption plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/opa/embedded"               // OPA authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/openapi-validator/embedded" // OpenAPI validator plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/saml/embedded"              // SAML SP plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/go-bundle/waf/embedded"               // WAF plugin.
)
