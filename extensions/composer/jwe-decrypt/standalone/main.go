package main

import (
	shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt"
)

// WellKnownHttpFilterConfigFactories is the plugin entry point when running it as an
// independently loaded composer plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return impl.WellKnownHttpFilterConfigFactories()
}
