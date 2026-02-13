// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package host contains the code to register the plugin with the host binary.
package host

import (
	oauth2te "github.com/tetratelabs/built-on-envoy/extensions/composer/oauth2te"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
)

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(oauth2te.WellKnownHttpFilterConfigFactories())
}
