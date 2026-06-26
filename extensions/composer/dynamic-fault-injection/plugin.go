// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the dynamic-fault-injection extension.
package impl

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

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
		config    *fault.FilterConfig
		endpoints []endpointEntry
	}

	// latencyFaultFilter implements [shared.HttpFilter].
	// It operates as an upstream HTTP filter: it lets the request flow to the upstream,
	// then on response measures actual elapsed time and injects only the remaining delay
	// needed to match the target distribution.
	latencyFaultFilter struct {
		handle  shared.HttpFilterHandle
		factory *latencyFaultFilterFactory

		// Populated during OnRequestHeaders.
		sample       fault.ResponseSample
		matched      bool
		requestStart time.Time

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

// headerMapAdapter adapts shared.HeaderMap to fault.HeaderGetter.
type headerMapAdapter struct {
	headers shared.HeaderMap
}

func (a *headerMapAdapter) GetOne(name string) string {
	return a.headers.GetOne(name).ToUnsafeString()
}

// OnRequestHeaders is called when the request is flowing to the upstream.
// We match the route, sample from the distribution, and record the start time.
func (f *latencyFaultFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	path := headers.GetOne(":path").ToUnsafeString()

	// Find the matching endpoint and sample.
	adapter := &headerMapAdapter{headers: headers}
	for i := range f.factory.endpoints {
		ep := &f.factory.endpoints[i]
		if !fault.MatchRoute(ep.match, path, adapter) {
			continue
		}

		// Matched endpoint — sample a response.
		if ep.distribution != nil {
			f.sample = ep.distribution.Sample()
			f.matched = true
		} else if ep.loadBased != nil {
			// TODO: Feed actual RPS when tracking is implemented.
			f.sample = ep.loadBased.Sample(0)
			f.matched = true
		}
		break
	}

	// Record when the request was sent to upstream.
	if f.matched {
		f.requestStart = time.Now()
	}

	// Always let the request proceed to the upstream.
	return shared.HeadersStatusContinue
}

// OnResponseHeaders is called when the response arrives from the upstream.
// We calculate how much time the upstream actually took, then inject only
// the remaining delay (target - actual) to match the sampled distribution.
func (f *latencyFaultFilter) OnResponseHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	if !f.matched {
		return shared.HeadersStatusContinue
	}

	elapsed := time.Since(f.requestStart)
	remainingDelay := f.sample.Duration - elapsed
	if remainingDelay < 0 {
		remainingDelay = 0
	}

	// If the sampled status is an error (4xx/5xx), override the upstream response.
	if f.sample.Status >= 400 {
		if remainingDelay > 0 {
			// Delay, then send local error response.
			scheduler := f.handle.GetScheduler()
			sample := f.sample
			totalDuration := f.sample.Duration
			go func() {
				time.Sleep(remainingDelay)
				scheduler.Schedule(func() {
					f.handle.SendLocalResponse(
						uint32(sample.Status),
						[][2]string{
							{"Content-Type", "text/plain"},
							{"x-fault-injected", "abort"},
							{"x-fault-injected-delay", totalDuration.String()},
							{"x-fault-actual-upstream", elapsed.String()},
							{"x-fault-added-delay", remainingDelay.String()},
							{"x-fault-status", fmt.Sprintf("%d", sample.Status)},
						},
						[]byte(fmt.Sprintf("fault filter abort: %d\n", sample.Status)),
						"fault_abort",
					)
				})
			}()
			return shared.HeadersStatusStopAllAndBuffer
		}

		// No remaining delay needed — immediate abort.
		f.handle.SendLocalResponse(
			uint32(f.sample.Status),
			[][2]string{
				{"Content-Type", "text/plain"},
				{"x-fault-injected", "abort"},
				{"x-fault-injected-delay", f.sample.Duration.String()},
				{"x-fault-actual-upstream", elapsed.String()},
				{"x-fault-status", fmt.Sprintf("%d", f.sample.Status)},
			},
			[]byte(fmt.Sprintf("fault filter abort: %d\n", f.sample.Status)),
			"fault_abort",
		)
		return shared.HeadersStatusStop
	}

	// For success status codes: add metadata headers and delay if needed.
	headers.Set("x-fault-injected-delay", f.sample.Duration.String())
	headers.Set("x-fault-actual-upstream", elapsed.String())
	headers.Set("x-fault-status", fmt.Sprintf("%d", f.sample.Status))
	if remainingDelay > 0 {
		headers.Set("x-fault-added-delay", remainingDelay.String())
	}

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
	handle.Log(shared.LogLevelInfo, fmt.Sprintf("dynamic-fault-injection: initialized with %d endpoints (upstream mode)", len(factory.endpoints)))
	return factory, nil
}

// CreatePerRoute parses per-route configuration for the dynamic-fault-injection filter.
func (f *CustomHttpFilterConfigFactory) CreatePerRoute(unparsedConfig []byte) (any, error) {
	return buildFilterFactory(unparsedConfig)
}

// buildFilterFactory parses config and builds the filter factory with pre-computed distributions.
func buildFilterFactory(config []byte) (*latencyFaultFilterFactory, error) {
	cfg, err := fault.ParseConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	factory := &latencyFaultFilterFactory{
		config: cfg,
	}

	// Build per-endpoint distributions.
	for i, ep := range cfg.Endpoints {
		entry := endpointEntry{
			match: ep.Match,
		}

		// Build the simple response distribution if responses are configured.
		if len(ep.Responses) > 0 {
			dist, err := fault.NewResponseDistribution(ep.Responses, rng)
			if err != nil {
				return nil, fmt.Errorf("endpoint %d: failed to build response distribution: %w", i, err)
			}
			entry.distribution = dist
		}

		// Build the load-based distribution if configured.
		if ep.LoadBased != nil {
			lb, err := fault.NewLoadBasedResponseDistribution(
				ep.LoadBased.Healthy.Responses,
				ep.LoadBased.Healthy.ThresholdRPS,
				ep.LoadBased.TippingPoint.Responses,
				ep.LoadBased.TippingPoint.ThresholdRPS,
				ep.LoadBased.GreyZone,
				rng,
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
