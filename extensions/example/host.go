package example

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	impl "github.com/tetratelabs/built-on-envoy/extensions/example/impl"
)

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories())
}
