// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main provides the entry point for the standalone WAF plugin.
package main

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/waf"
)

func WellKnownHttpFilterConfigFactories() map[string]sdk.HttpFilterConfigFactory { // nolint:revive
	return impl.WellKnownHttpFilterConfigFactories() // nolint:revive
}
