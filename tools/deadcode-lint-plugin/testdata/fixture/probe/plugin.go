// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This fixture distinguishes externally dispatched callbacks from disconnected code.
package probe

import "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

var value int

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{"probe": &configFactory{}}
}

type configFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (*configFactory) Create(h shared.HttpFilterConfigHandle, _ []byte) (shared.HttpFilterFactory, error) {
	h.GetScheduler().Schedule(configScheduled)
	h.HttpCallout("cluster", nil, nil, 0, &callout{})
	h.StartHttpStream("cluster", nil, nil, false, 0, &stream{})
	return &factory{}, nil
}
func (*configFactory) CreatePerRoute([]byte) (any, error) { perRouteHelper(); return nil, nil }

type factory struct{ shared.EmptyHttpFilterFactory }

func (*factory) Create(h shared.HttpFilterHandle) shared.HttpFilter { return &filter{handle: h} }

type filter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
}

func (f *filter) OnRequestHeaders(shared.HeaderMap, bool) shared.HeadersStatus {
	f.handle.GetScheduler().Schedule(streamScheduled)
	f.handle.HttpCallout("cluster", nil, nil, 0, &callout{})
	f.handle.StartHttpStream("cluster", nil, nil, false, 0, &stream{})
	f.handle.SetDownstreamWatermarkCallbacks(&watermark{})
	genericLive[int](1)
	return shared.HeadersStatusContinue
}

type callout struct{}

func (*callout) OnHttpCalloutDone(uint64, shared.HttpCalloutResult, [][2]shared.UnsafeEnvoyBuffer, []shared.UnsafeEnvoyBuffer) {
	calloutHelper()
}

type stream struct{}

func (*stream) OnHttpStreamHeaders(uint64, [][2]shared.UnsafeEnvoyBuffer, bool) {
	streamHeadersHelper()
}
func (*stream) OnHttpStreamData(uint64, []shared.UnsafeEnvoyBuffer, bool)  { streamDataHelper() }
func (*stream) OnHttpStreamTrailers(uint64, [][2]shared.UnsafeEnvoyBuffer) { streamTrailersHelper() }

func (*stream) OnHttpStreamComplete(uint64)                            { streamCompleteHelper() }
func (*stream) OnHttpStreamReset(uint64, shared.HttpStreamResetReason) { streamResetHelper() }

type watermark struct{}

func (*watermark) OnAboveWriteBufferHighWatermark() { highHelper() }
func (*watermark) OnBelowWriteBufferLowWatermark()  { lowHelper() }
func configScheduled()                              { value++ }
func streamScheduled()                              { value++ }
func perRouteHelper()                               { value++ }
func calloutHelper()                                { value++ }
func streamHeadersHelper()                          { value++ }
func streamDataHelper()                             { value++ }
func streamTrailersHelper()                         { value++ }
func streamCompleteHelper()                         { value++ }
func streamResetHelper()                            { value++ }
func highHelper()                                   { value++ }
func lowHelper()                                    { value++ }
func genericLive[T any](v T) T                      { return v }
func ExportedTestOnly()                             { value++ }
func unusedHelper()                                 { value++ }

type unregisteredFilter struct{ shared.EmptyHttpFilter }

func (*unregisteredFilter) OnRequestHeaders(shared.HeaderMap, bool) shared.HeadersStatus {
	return shared.HeadersStatusContinue
}
