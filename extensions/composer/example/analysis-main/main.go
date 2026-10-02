// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build analysis

// Package main models Envoy's external lifecycle calls for production reachability
// analysis. It is compiled and analyzed, never executed, so arguments are placeholders.
package main

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/example"
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/example/embedded"
)

func main() {
	for name := range impl.WellKnownHttpFilterConfigFactories() {
		configFactory := sdk.GetHttpFilterConfigFactory(name)
		perRoute, err := configFactory.CreatePerRoute(nil)
		if err != nil {
			panic(err)
		}
		factory, err := sdk.NewHttpFilterFactory(nil, name, nil)
		if err != nil {
			panic(err)
		}
		filter := factory.Create(analysisHandle{perRoute: perRoute})
		filter.OnRequestHeaders(nil, false)
		filter.OnRequestBody(nil, false)
		filter.OnRequestTrailers(nil)
		filter.OnResponseHeaders(nil, false)
		filter.OnResponseBody(nil, false)
		filter.OnResponseTrailers(nil)
		filter.OnLocalReply(0, shared.UnsafeEnvoyBuffer{}, false)
		filter.OnStreamComplete()
		filter.OnDestroy()
		factory.OnDestroy()
	}
}

type analysisHandle struct {
	shared.HttpFilterHandle
	perRoute any
}

func (h analysisHandle) GetMostSpecificConfig() any {
	return h.perRoute
}

func (analysisHandle) GetScheduler() shared.Scheduler {
	return analysisScheduler{}
}

type analysisScheduler struct{}

// SDK scheduling crosses the host boundary; model dispatch to retain callback bodies.
func (analysisScheduler) Schedule(callback func()) {
	callback()
}
