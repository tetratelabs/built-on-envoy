// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the dynamic-fault-injection extension.
package impl

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

const (
	requestsInFlightHeader = "x-fault-requests-in-flight"
	addedDelayHeader       = "x-fault-added-delay"
	actualUpstreamHeader   = "x-fault-actual-upstream"
	injectedDelayHeader    = "x-fault-injected-delay"
	injectedHeader         = "x-fault-injected"
	statusHeader           = "x-fault-status"
	workerIndexHeader      = "x-fault-worker-index"
)

var activeRequests atomic.Int64

type (
	// endpointEntry holds a compiled endpoint with its response distribution.
	endpointEntry struct {
		match        fault.MatchConfig
		distribution *fault.ResponseDistribution
		loadBased    *fault.LoadBasedResponseDistribution
	}

	// latencyFaultFilterFactory implements [shared.HttpFilterFactory].
	// It holds the parsed config and pre-built response distributions.
	latencyFaultFilterFactory struct {
		shared.EmptyHttpFilterFactory
		config       *fault.FilterConfig
		endpoints    []endpointEntry
		distribution *fault.ResponseDistribution
		loadBased    *fault.LoadBasedResponseDistribution
		direct       bool
	}

	// latencyFaultFilter implements [shared.HttpFilter].
	// It operates as an upstream HTTP filter: it lets the request flow to the upstream,
	// then on response measures actual elapsed time and injects only the remaining delay
	// needed to match the target distribution.
	latencyFaultFilter struct {
		handle  shared.HttpFilterHandle
		factory *latencyFaultFilterFactory

		// Populated during OnRequestHeaders.
		sample               fault.ResponseSample
		matched              bool
		counted              bool
		requestEntryInFlight int64
		requestStart         time.Time

		shared.EmptyHttpFilter
	}
)

// Create implements [shared.HttpFilterFactory].
func (f *latencyFaultFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	factory := f
	if perRoute := getMostSpecificConfig[*latencyFaultFilterFactory](handle); perRoute != nil {
		factory = perRoute
	}
	return &latencyFaultFilter{handle: handle, factory: factory}
}

func (f *latencyFaultFilterFactory) sample(loadValue int64) (fault.ResponseSample, bool) {
	if f.direct {
		if f.distribution != nil {
			return f.distribution.Sample(), true
		}
		if f.loadBased != nil {
			return f.loadBased.Sample(float64(loadValue)), true
		}
		return fault.ResponseSample{}, false
	}
	return fault.ResponseSample{}, false
}

// headerMapAdapter adapts shared.HeaderMap to fault.HeaderGetter.
type headerMapAdapter struct {
	headers shared.HeaderMap
}

func (a *headerMapAdapter) GetOne(name string) string {
	return a.headers.GetOne(name).ToUnsafeString()
}

// OnRequestHeaders is called when the request is flowing to the upstream.
// We match the route, sample from the distribution, and record the start time.
func (f *latencyFaultFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	loadValue := activeRequests.Load()
	if sample, ok := f.factory.sample(loadValue); ok {
		f.sample = sample
		f.matched = true
	} else {
		path := headers.GetOne(":path").ToUnsafeString()
		adapter := &headerMapAdapter{headers: headers}
		for i := range f.factory.endpoints {
			ep := &f.factory.endpoints[i]
			if !fault.MatchRoute(ep.match, path, adapter) {
				continue
			}
			if ep.distribution != nil {
				f.sample = ep.distribution.Sample()
				f.matched = true
			} else if ep.loadBased != nil {
				f.sample = ep.loadBased.Sample(float64(loadValue))
				f.matched = true
			}
			break
		}
	}

	// Record when the request was sent to upstream.
	if f.matched {
		f.requestEntryInFlight = loadValue
		f.requestStart = time.Now()
		f.startRequest()
	}

	// Always let the request proceed to the upstream.
	return shared.HeadersStatusContinue
}

// OnResponseHeaders is called when the response arrives from the upstream.
// We calculate how much time the upstream actually took, then inject only
// the remaining delay (target - actual) to match the sampled distribution.
func (f *latencyFaultFilter) OnResponseHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if !f.matched {
		return shared.HeadersStatusContinue
	}

	elapsed := time.Since(f.requestStart)
	remainingDelay := max(f.sample.Duration-elapsed, 0)

	status := headers.GetOne(":status").ToUnsafeString()
	upstreamStatus, err := strconv.Atoi(status)
	if err != nil {
		return shared.HeadersStatusContinue
	}
	requestEntryInFlight := strconv.FormatInt(f.requestEntryInFlight, 10)

	// If this upstream status has no configured behavior, pass through untouched.
	if f.sample.Status < 400 && upstreamStatus != f.sample.Status {
		headers.Set(injectedDelayHeader, "0s")
		headers.Set(actualUpstreamHeader, elapsed.String())
		headers.Set(addedDelayHeader, "0s")
		headers.Set(statusHeader, status)
		headers.Set(requestsInFlightHeader, requestEntryInFlight)
		f.setDiagnosticHeaders(headers)
		return shared.HeadersStatusContinue
	}
	// If the sampled status is an "error" case and different from the upstream response rewrite the response
	if f.sample.Status >= 400 && f.sample.Status != upstreamStatus {
		if remainingDelay > 0 {
			// Delay, then send local error response.
			scheduler := f.handle.GetScheduler()
			sample := f.sample
			totalDuration := f.sample.Duration
			responseHeaders := [][2]string{
				{"Content-Type", "text/plain"},
				{injectedHeader, "abort"},
				{injectedDelayHeader, totalDuration.String()},
				{actualUpstreamHeader, elapsed.String()},
				{addedDelayHeader, remainingDelay.String()},
				{statusHeader, fmt.Sprintf("%d", sample.Status)},
				{requestsInFlightHeader, requestEntryInFlight},
			}
			responseHeaders = f.withDiagnosticHeaders(responseHeaders)
			go func() {
				time.Sleep(remainingDelay)
				scheduler.Schedule(func() {
					f.handle.SendLocalResponse(
						uint32(sample.Status), //nolint:gosec // Status is validated to be 100-599 by ParseConfig
						responseHeaders,
						[]byte(fmt.Sprintf("fault filter abort: %d\n", sample.Status)),
						"fault_abort",
					)
				})
			}()
			return shared.HeadersStatusStopAllAndBuffer
		}

		// No remaining delay needed — immediate abort.
		responseHeaders := [][2]string{
			{"Content-Type", "text/plain"},
			{injectedHeader, "abort"},
			{injectedDelayHeader, f.sample.Duration.String()},
			{actualUpstreamHeader, elapsed.String()},
			{statusHeader, fmt.Sprintf("%d", f.sample.Status)},
			{requestsInFlightHeader, requestEntryInFlight},
		}
		responseHeaders = f.withDiagnosticHeaders(responseHeaders)
		f.handle.SendLocalResponse(
			uint32(f.sample.Status), //nolint:gosec // Status is validated to be 100-599 by ParseConfig
			responseHeaders,
			[]byte(fmt.Sprintf("fault filter abort: %d\n", f.sample.Status)),
			"fault_abort",
		)
		return shared.HeadersStatusStop
	}

	// For all expected status codes: add metadata headers and delay if needed.
	headers.Set(injectedDelayHeader, f.sample.Duration.String())
	headers.Set(actualUpstreamHeader, elapsed.String())
	headers.Set(statusHeader, fmt.Sprintf("%d", f.sample.Status))
	headers.Set(requestsInFlightHeader, requestEntryInFlight)
	f.setDiagnosticHeaders(headers)
	if remainingDelay > 0 {
		headers.Set(addedDelayHeader, remainingDelay.String())
	}

	// Is it worth saving the additional schedule in the case `remainingDelay == 0`?
	if remainingDelay > 0 {
		// Delay the response before continuing to downstream.
		scheduler := f.handle.GetScheduler()
		go func() {
			time.Sleep(remainingDelay)
			scheduler.Schedule(func() {
				f.handle.ContinueResponse()
			})
		}()
		return shared.HeadersStatusStopAllAndBuffer
	}

	// Upstream was already slow enough — no additional delay needed.
	return shared.HeadersStatusContinue
}

func (f *latencyFaultFilter) setDiagnosticHeaders(headers shared.HeaderMap) {
	if !f.diagnostic() {
		return
	}
	headers.Set(workerIndexHeader, strconv.FormatUint(uint64(f.handle.GetWorkerIndex()), 10))
}

func (f *latencyFaultFilter) withDiagnosticHeaders(headers [][2]string) [][2]string {
	if !f.diagnostic() {
		return headers
	}
	return append(headers, [2]string{workerIndexHeader, strconv.FormatUint(uint64(f.handle.GetWorkerIndex()), 10)})
}

func (f *latencyFaultFilter) diagnostic() bool {
	return f.factory != nil && f.factory.config != nil && f.factory.config.Diagnostic
}

// OnStreamComplete releases the request from the global in-flight count.
func (f *latencyFaultFilter) OnStreamComplete() {
	f.releaseRequest()
}

// OnDestroy releases the request if Envoy destroys the filter without first
// calling OnStreamComplete.
func (f *latencyFaultFilter) OnDestroy() {
	f.releaseRequest()
}

func (f *latencyFaultFilter) releaseRequest() {
	if !f.counted {
		return
	}
	f.counted = false
	activeRequests.Add(-1)
}

func (f *latencyFaultFilter) startRequest() {
	if f.counted {
		return
	}
	f.counted = true
	activeRequests.Add(1)
}

// CustomHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type CustomHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create implements [shared.HttpFilterConfigFactory].
func (f *CustomHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	factory, err := buildFilterFactory(config)
	if err != nil {
		handle.Log(shared.LogLevelError, "dynamic-fault-injection: "+err.Error())
		return nil, err
	}
	mode := "upstream mode"
	if factory.direct {
		mode = "direct mode"
	}
	handle.Log(shared.LogLevelInfo, fmt.Sprintf("dynamic-fault-injection: initialized in %s with %d endpoints", mode, len(factory.endpoints)))
	return factory, nil
}

// CreatePerRoute parses per-route configuration for the dynamic-fault-injection filter.
func (f *CustomHttpFilterConfigFactory) CreatePerRoute(unparsedConfig []byte) (any, error) {
	return buildFilterFactoryForSource(unparsedConfig, fault.PerRouteConfigSource)
}

// buildFilterFactory parses config and builds the filter factory with pre-computed distributions.
func buildFilterFactory(config []byte) (*latencyFaultFilterFactory, error) {
	return buildFilterFactoryForSource(config, fault.FilterConfigSource)
}

func buildFilterFactoryForSource(config []byte, source fault.ConfigSource) (*latencyFaultFilterFactory, error) {
	var cfg *fault.FilterConfig
	var err error
	if source == fault.PerRouteConfigSource {
		cfg, err = fault.ParsePerRouteConfig(config)
	} else {
		cfg, err = fault.ParseConfig(config)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	factory := &latencyFaultFilterFactory{
		config: cfg,
		direct: source == fault.PerRouteConfigSource || len(cfg.Endpoints) == 0,
	}
	if factory.direct {
		if len(cfg.Responses) > 0 {
			factory.distribution, err = fault.NewResponseDistributionWithMode(cfg.Responses, cfg.ProbabilityDistribution)
			if err != nil {
				return nil, fmt.Errorf("failed to build response distribution: %w", err)
			}
		}
		if cfg.LoadBased != nil {
			factory.loadBased, err = fault.NewLoadBasedResponseDistributionWithMode(
				cfg.LoadBased.Healthy.Responses,
				cfg.LoadBased.Healthy.ThresholdRPS,
				cfg.LoadBased.TippingPoint.Responses,
				cfg.LoadBased.TippingPoint.ThresholdRPS,
				cfg.LoadBased.GreyZone,
				cfg.ProbabilityDistribution,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to build load-based distribution: %w", err)
			}
		}
		return factory, nil
	}

	// Build per-endpoint distributions.
	for i, ep := range cfg.Endpoints {
		entry := endpointEntry{
			match: ep.Match,
		}

		// Build the simple response distribution if responses are configured.
		if len(ep.Responses) > 0 {
			dist, err := fault.NewResponseDistributionWithMode(ep.Responses, cfg.ProbabilityDistribution)
			if err != nil {
				return nil, fmt.Errorf("endpoint %d: failed to build response distribution: %w", i, err)
			}
			entry.distribution = dist
		}

		// Build the load-based distribution if configured.
		if ep.LoadBased != nil {
			lb, err := fault.NewLoadBasedResponseDistributionWithMode(
				ep.LoadBased.Healthy.Responses,
				ep.LoadBased.Healthy.ThresholdRPS,
				ep.LoadBased.TippingPoint.Responses,
				ep.LoadBased.TippingPoint.ThresholdRPS,
				ep.LoadBased.GreyZone,
				cfg.ProbabilityDistribution,
			)
			if err != nil {
				return nil, fmt.Errorf("endpoint %d: failed to build load-based distribution: %w", i, err)
			}
			entry.loadBased = lb
		}

		factory.endpoints = append(factory.endpoints, entry)
	}

	return factory, nil
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		"dynamic-fault-injection": &CustomHttpFilterConfigFactory{},
	}
}

// getMostSpecificConfig returns the per-route config of type T from the filter handle, or the zero value.
func getMostSpecificConfig[T any](handle shared.HttpFilterHandle) T { //nolint:revive
	var zero T
	c := handle.GetMostSpecificConfig()
	if c == nil {
		return zero
	}
	cfg, ok := c.(T)
	if !ok {
		handle.Log(shared.LogLevelDebug, "dynamic-fault-injection: most specific config is not of expected type")
		return zero
	}
	return cfg
}
