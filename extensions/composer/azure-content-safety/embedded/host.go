// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package embedded registers the Azure Content Safety plugin with the Envoy dynamic module SDK.
package embedded

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/azure-content-safety"
)

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories()) //nolint:revive
}
