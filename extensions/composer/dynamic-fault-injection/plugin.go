// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the dynamic-fault-injection extension.
package impl

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/config"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

const (
	requestsInFlightHeader = "x-fault-requests-in-flight"
	addedDelayHeader       = "x-fault-added-delay"
	actualUpstreamHeader   = "x-fault-actual-upstream"
	injectedDelayHeader    = "x-fault-injected-delay"
	injectedHeader         = "x-fault-injected"
	statusHeader           = "x-fault-status"
	upstreamStatusHeader   = "x-fault-upstream-status"
	workerIndexHeader      = "x-fault-worker-index"

	// Span tag names (prefixed with "fault.")
	requestsInFlightTag = "fault.requests-in-flight"
	addedDelayTag       = "fault.added-delay"
	actualUpstreamTag   = "fault.actual-upstream"
	injectedDelayTag    = "fault.injected-delay"
	injectedTag         = "fault.injected"
	statusTag           = "fault.status"
	upstreamStatusTag   = "fault.upstream-status"
	workerIndexTag      = "fault.worker-index"
)

var activeRequests atomic.Int64

type (
	// endpointEntry holds a compiled endpoint with its response distribution.
	endpointEntry struct {
		match        config.MatchConfig
		distribution *fault.ResponseDistribution
		loadBased    *fault.LoadBasedResponseDistribution
	}

	// latencyFaultFilterFactory implements [shared.HttpFilterFactory].
	// It holds the parsed config and pre-built response distributions.
	latencyFaultFilterFactory struct {
		shared.EmptyHttpFilterFactory
		config       *config.FilterConfig
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

	// faultAttributes holds the fault injection metadata to be set on headers and spans.
	faultAttributes struct {
		RequestsInFlight int64
		AddedDelay       string
		ActualUpstream   string
		InjectedDelay    string
		Injected         string
		Status           string
		UpstreamStatus   string
		WorkerIndex      string
	}
)

// Fixed millisecond precision keeps timing headers and span tags stable between requests.
func formatTiming(duration time.Duration) string {
	return fmt.Sprintf("%.3fms", float64(duration)/float64(time.Millisecond))
}

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

	// Build base attributes for this response
	var workerIndex string
	if f.diagnostic() {
		workerIndex = strconv.FormatUint(uint64(f.handle.GetWorkerIndex()), 10)
	}

	if f.sample.Status != upstreamStatus {
		if remainingDelay > 0 {
			// Delay, then send the sampled status as a local response.
			scheduler := f.handle.GetScheduler()
			sample := f.sample
			totalDuration := f.sample.Duration

			attrs := faultAttributes{
				Injected:         injectedResponseName(sample.Status),
				InjectedDelay:    formatTiming(totalDuration),
				ActualUpstream:   formatTiming(elapsed),
				AddedDelay:       formatTiming(remainingDelay),
				Status:           fmt.Sprintf("%d", sample.Status),
				UpstreamStatus:   status,
				RequestsInFlight: f.requestEntryInFlight,
				WorkerIndex:      workerIndex,
			}
			body, responseHeaders := f.localResponse(sample, &attrs)

			go func() {
				time.Sleep(remainingDelay)
				scheduler.Schedule(func() {
					f.handle.SendLocalResponse(
						uint32(sample.Status), //nolint:gosec // Status is validated to be 200-599 by the config loader.
						responseHeaders,
						body,
						localResponseDetail(sample.Status),
					)
				})
			}()
			return shared.HeadersStatusStopAllAndBuffer
		}

		// No remaining delay needed; send the sampled status immediately.
		attrs := faultAttributes{
			Injected:         injectedResponseName(f.sample.Status),
			InjectedDelay:    formatTiming(f.sample.Duration),
			ActualUpstream:   formatTiming(elapsed),
			Status:           fmt.Sprintf("%d", f.sample.Status),
			UpstreamStatus:   status,
			RequestsInFlight: f.requestEntryInFlight,
			WorkerIndex:      workerIndex,
		}

		body, responseHeaders := f.localResponse(f.sample, &attrs)

		f.handle.SendLocalResponse(
			uint32(f.sample.Status), //nolint:gosec // Status is validated to be 200-599 by the config loader.
			responseHeaders,
			body,
			localResponseDetail(f.sample.Status),
		)
		return shared.HeadersStatusStop
	}

	// Matching statuses retain the upstream response and only receive any remaining delay.
	attrs := faultAttributes{
		InjectedDelay:    formatTiming(f.sample.Duration),
		ActualUpstream:   formatTiming(elapsed),
		Status:           fmt.Sprintf("%d", f.sample.Status),
		UpstreamStatus:   status,
		RequestsInFlight: f.requestEntryInFlight,
		WorkerIndex:      workerIndex,
	}
	if remainingDelay > 0 {
		attrs.AddedDelay = formatTiming(remainingDelay)
	}

	f.setFaultAttributesOnHeaderMap(headers, &attrs)

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

func (f *latencyFaultFilter) localResponse(sample fault.ResponseSample, attrs *faultAttributes) ([]byte, [][2]string) {
	body := []byte(fmt.Sprintf("fault filter abort: %d\n", sample.Status))
	if sample.Status < 400 {
		body = []byte{}
	}
	responseHeaders := make([][2]string, 0, 1)
	configuredContentType := false
	if sample.LocalResponse != nil {
		if sample.LocalResponse.Body != nil {
			body = []byte(*sample.LocalResponse.Body)
		}
		for _, header := range sample.LocalResponse.Headers {
			if strings.EqualFold(header.Name, "content-type") {
				configuredContentType = true
			}
			responseHeaders = append(responseHeaders, [2]string{strings.ToLower(header.Name), header.Value})
		}
	}
	if !configuredContentType {
		responseHeaders = append([][2]string{{"content-type", "text/plain"}}, responseHeaders...)
	}
	return body, f.setFaultAttributesOnHeaderArray(responseHeaders, attrs)
}

func injectedResponseName(status int) string {
	if status < 400 {
		return "response"
	}
	return "abort"
}

func localResponseDetail(status int) string {
	if status < 400 {
		return "fault_response"
	}
	return "fault_abort"
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
func (f *CustomHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	factory, err := buildFilterFactory(data)
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
	return buildFilterFactoryForSource(unparsedConfig, config.PerRouteSource)
}

// buildFilterFactory parses config and builds the filter factory with pre-computed distributions.
func buildFilterFactory(data []byte) (*latencyFaultFilterFactory, error) {
	return buildFilterFactoryForSource(data, config.FilterSource)
}

func buildFilterFactoryForSource(data []byte, source config.Source) (*latencyFaultFilterFactory, error) {
	cfg, err := filterConfigLoader.Load(data, source)
	if err != nil {
		return nil, err
	}

	factory := &latencyFaultFilterFactory{
		config: cfg,
		direct: source == config.PerRouteSource || len(cfg.Endpoints) == 0,
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
				cfg.LoadBased.Healthy.ThresholdInFlight,
				cfg.LoadBased.TippingPoint.Responses,
				cfg.LoadBased.TippingPoint.ThresholdInFlight,
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
				ep.LoadBased.Healthy.ThresholdInFlight,
				ep.LoadBased.TippingPoint.Responses,
				ep.LoadBased.TippingPoint.ThresholdInFlight,
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

// setFaultAttributesOnHeaderMap sets fault injection attributes on response headers and active span tags.
// Headers are always set; span tags are only set if an active span is available.
func (f *latencyFaultFilter) setFaultAttributesOnHeaderMap(headers shared.HeaderMap, attrs *faultAttributes) {
	// Set headers
	if attrs.InjectedDelay != "" {
		headers.Set(injectedDelayHeader, attrs.InjectedDelay)
	}
	if attrs.ActualUpstream != "" {
		headers.Set(actualUpstreamHeader, attrs.ActualUpstream)
	}
	if attrs.AddedDelay != "" {
		headers.Set(addedDelayHeader, attrs.AddedDelay)
	}
	if attrs.Status != "" {
		headers.Set(statusHeader, attrs.Status)
	}
	if attrs.UpstreamStatus != "" {
		headers.Set(upstreamStatusHeader, attrs.UpstreamStatus)
	}
	if attrs.RequestsInFlight >= 0 {
		headers.Set(requestsInFlightHeader, strconv.FormatInt(attrs.RequestsInFlight, 10))
	}
	if attrs.Injected != "" {
		headers.Set(injectedHeader, attrs.Injected)
	}
	if attrs.WorkerIndex != "" {
		headers.Set(workerIndexHeader, attrs.WorkerIndex)
	}

	f.setFaultSpanAttributes(attrs)
}

// setFaultSpanAttributes sets fault injection attributes on the active span, if one exists.
func (f *latencyFaultFilter) setFaultSpanAttributes(attrs *faultAttributes) {
	span := f.handle.GetActiveSpan()
	if span == nil {
		return
	}
	if attrs.InjectedDelay != "" {
		span.SetTag(injectedDelayTag, attrs.InjectedDelay)
	}
	if attrs.ActualUpstream != "" {
		span.SetTag(actualUpstreamTag, attrs.ActualUpstream)
	}
	if attrs.AddedDelay != "" {
		span.SetTag(addedDelayTag, attrs.AddedDelay)
	}
	if attrs.Status != "" {
		span.SetTag(statusTag, attrs.Status)
	}
	if attrs.UpstreamStatus != "" {
		span.SetTag(upstreamStatusTag, attrs.UpstreamStatus)
	}
	if attrs.RequestsInFlight >= 0 {
		span.SetTag(requestsInFlightTag, strconv.FormatInt(attrs.RequestsInFlight, 10))
	}
	if attrs.Injected != "" {
		span.SetTag(injectedTag, attrs.Injected)
	}
	if attrs.WorkerIndex != "" {
		span.SetTag(workerIndexTag, attrs.WorkerIndex)
	}
}

// setFaultAttributesOnHeaderArray sets fault injection attributes on a header array
// and the active span, typically for local responses. It returns the modified headers.
func (f *latencyFaultFilter) setFaultAttributesOnHeaderArray(headers [][2]string, attrs *faultAttributes) [][2]string {
	if attrs.Injected != "" {
		headers = append(headers, [2]string{injectedHeader, attrs.Injected})
	}
	if attrs.InjectedDelay != "" {
		headers = append(headers, [2]string{injectedDelayHeader, attrs.InjectedDelay})
	}
	if attrs.ActualUpstream != "" {
		headers = append(headers, [2]string{actualUpstreamHeader, attrs.ActualUpstream})
	}
	if attrs.AddedDelay != "" {
		headers = append(headers, [2]string{addedDelayHeader, attrs.AddedDelay})
	}
	if attrs.Status != "" {
		headers = append(headers, [2]string{statusHeader, attrs.Status})
	}
	if attrs.UpstreamStatus != "" {
		headers = append(headers, [2]string{upstreamStatusHeader, attrs.UpstreamStatus})
	}
	if attrs.RequestsInFlight >= 0 {
		headers = append(headers, [2]string{requestsInFlightHeader, strconv.FormatInt(attrs.RequestsInFlight, 10)})
	}
	if attrs.WorkerIndex != "" {
		headers = append(headers, [2]string{workerIndexHeader, attrs.WorkerIndex})
	}

	f.setFaultSpanAttributes(attrs)

	return headers
}
