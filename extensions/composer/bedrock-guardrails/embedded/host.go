// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package host contains the code to register the plugin with the host binary.
package host

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/bedrock-guardrails"
)

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories())
}
