// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This fixture exercises real SDK registration for isolated host reachability.
package host

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"

	impl "github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/fixture/probe"
)

func init() { sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories()) }
