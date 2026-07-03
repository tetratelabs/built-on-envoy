// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main is the entry point for the standalone version of the web-terminal extension.
package main

import (
	shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/web-terminal"
)

// WellKnownNetworkFilterConfigFactories is the plugin entry point when running it as an
// independently loaded composer plugin.
func WellKnownNetworkFilterConfigFactories() map[string]shared.NetworkFilterConfigFactory { //nolint:revive
	return impl.WellKnownNetworkFilterConfigFactories()
}
