package host

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt"
)

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories())
}
