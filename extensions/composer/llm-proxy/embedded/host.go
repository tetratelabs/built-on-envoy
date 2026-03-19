// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package embedded registers the llm-proxy plugin with the Envoy dynamic module SDK.
package embedded

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"

	llmproxy "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy"
)

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(llmproxy.WellKnownHttpFilterConfigFactories()) //nolint:revive
}
