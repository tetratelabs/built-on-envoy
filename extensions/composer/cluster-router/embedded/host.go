// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package host registers the cluster-router plugin into the composer host
// binary so it can be used without loading a shared library.
package host

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/cluster-router"
)

func init() {
	sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories())
}
