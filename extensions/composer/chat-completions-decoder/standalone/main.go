// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main provides the entry point for the standalone chat-completions-decoder plugin.
package main

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/chat-completions-decoder"
)

// WellKnownHttpFilterConfigFactories is the plugin entry point when running it as an
// independently loaded composer plugin.
func WellKnownHttpFilterConfigFactories() map[string]sdk.HttpFilterConfigFactory { //nolint:revive
	return impl.WellKnownHttpFilterConfigFactories()
}
