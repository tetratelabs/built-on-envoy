// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main builds a Go plugin that can be loaded by the composer dynamic module.
package main

import (
	shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/example"
)

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return impl.WellKnownHttpFilterConfigFactories()
}
