package main

import (
	shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	exampleImpl "github.com/tetratelabs/built-on-envoy/extensions/example/impl"
)

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	// Merge all well-known plugin config factories here if we have multiple plugins
	// in the future.
	var factories = make(map[string]shared.HttpFilterConfigFactory)
	for name, factory := range exampleImpl.WellKnownHttpFilterConfigFactories() {
		factories["goplugin_"+name] = factory
	}
	return factories
}
