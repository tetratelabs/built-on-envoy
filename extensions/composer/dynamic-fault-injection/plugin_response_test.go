// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

func TestPerRouteConfigOverride_WrongTypeUsesBaseFactory(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	baseFactory, err := buildFilterFactory(ValidConfig)
	require.NoError(t, err)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetMostSpecificConfig().Return("not a filter factory")
	handle.EXPECT().Log(shared.LogLevelDebug, "dynamic-fault-injection: most specific config is not of expected type")

	filter := baseFactory.Create(handle).(*latencyFaultFilter)
	require.Same(t, baseFactory, filter.factory)
}

func TestHeaderMapAdapter_GetOne(t *testing.T) {
	headers := fake.NewFakeHeaderMap(map[string][]string{"x-test": {"value"}})
	adapter := &headerMapAdapter{headers: headers}

	require.Equal(t, "value", adapter.GetOne("x-test"))
	require.Empty(t, adapter.GetOne("x-missing"))
}

func TestSetFaultAttributesOnHeaderMap_SetsHeadersAndSpanTagsOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	span := mocks.NewMockSpan(ctrl)
	handle.EXPECT().GetActiveSpan().Return(span).Times(1)
	expectFaultSpanTags(span)

	attrs := testFaultAttributes()
	headers := fake.NewFakeHeaderMap(nil)
	filter := &latencyFaultFilter{handle: handle}

	filter.setFaultAttributesOnHeaderMap(headers, &attrs)

	require.Equal(t, attrs.InjectedDelay, headers.GetOne(injectedDelayHeader).ToUnsafeString())
	require.Equal(t, attrs.ActualUpstream, headers.GetOne(actualUpstreamHeader).ToUnsafeString())
	require.Equal(t, attrs.AddedDelay, headers.GetOne(addedDelayHeader).ToUnsafeString())
	require.Equal(t, attrs.Status, headers.GetOne(statusHeader).ToUnsafeString())
	require.Equal(t, "7", headers.GetOne(requestsInFlightHeader).ToUnsafeString())
	require.Equal(t, attrs.Injected, headers.GetOne(injectedHeader).ToUnsafeString())
	require.Equal(t, attrs.WorkerIndex, headers.GetOne(workerIndexHeader).ToUnsafeString())
}

func TestSetFaultAttributesOnHeaderArray_SetsHeadersAndSpanTagsOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	span := mocks.NewMockSpan(ctrl)
	handle.EXPECT().GetActiveSpan().Return(span).Times(1)
	expectFaultSpanTags(span)

	attrs := testFaultAttributes()
	filter := &latencyFaultFilter{handle: handle}
	headers := filter.setFaultAttributesOnHeaderArray([][2]string{{"content-type", "text/plain"}}, &attrs)

	requireResponseHeader(t, headers, injectedDelayHeader, attrs.InjectedDelay)
	requireResponseHeader(t, headers, actualUpstreamHeader, attrs.ActualUpstream)
	requireResponseHeader(t, headers, addedDelayHeader, attrs.AddedDelay)
	requireResponseHeader(t, headers, statusHeader, attrs.Status)
	requireResponseHeader(t, headers, requestsInFlightHeader, "7")
	requireResponseHeader(t, headers, injectedHeader, attrs.Injected)
	requireResponseHeader(t, headers, workerIndexHeader, attrs.WorkerIndex)
	requireHeaderCount(t, headers, injectedDelayHeader, 1)
	requireHeaderCount(t, headers, actualUpstreamHeader, 1)
	requireHeaderCount(t, headers, addedDelayHeader, 1)
	requireHeaderCount(t, headers, statusHeader, 1)
	requireHeaderCount(t, headers, requestsInFlightHeader, 1)
	requireHeaderCount(t, headers, injectedHeader, 1)
	requireHeaderCount(t, headers, workerIndexHeader, 1)
}

func testFaultAttributes() faultAttributes {
	return faultAttributes{
		InjectedDelay:    "100ms",
		ActualUpstream:   "10ms",
		AddedDelay:       "90ms",
		Status:           "503",
		RequestsInFlight: 7,
		Injected:         "abort",
		WorkerIndex:      "3",
	}
}

func expectFaultSpanTags(span *mocks.MockSpan) {
	span.EXPECT().SetTag(injectedDelayTag, "100ms").Times(1)
	span.EXPECT().SetTag(actualUpstreamTag, "10ms").Times(1)
	span.EXPECT().SetTag(addedDelayTag, "90ms").Times(1)
	span.EXPECT().SetTag(statusTag, "503").Times(1)
	span.EXPECT().SetTag(requestsInFlightTag, "7").Times(1)
	span.EXPECT().SetTag(injectedTag, "abort").Times(1)
	span.EXPECT().SetTag(workerIndexTag, "3").Times(1)
}

func requireHeaderCount(t *testing.T, headers [][2]string, name string, expected int) {
	t.Helper()
	count := 0
	for _, header := range headers {
		if header[0] == name {
			count++
		}
	}
	require.Equal(t, expected, count, "header %q count", name)
}

func TestOnResponseHeaders_DelayedAbort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	activeRequests.Store(99)
	t.Cleanup(func() { activeRequests.Store(0) })
	scheduler := newResponseTestScheduler()
	handle.EXPECT().GetScheduler().Return(scheduler)

	var localResponseHeaders [][2]string
	handle.EXPECT().SendLocalResponse(
		uint32(503),
		gomock.Any(),
		[]byte("fault filter abort: 503\n"),
		"fault_abort",
	).Do(func(_ uint32, headers [][2]string, _ []byte, _ string) {
		localResponseHeaders = headers
	})

	filter := &latencyFaultFilter{
		handle:               handle,
		matched:              true,
		sample:               fault.ResponseSample{Status: 503, Duration: 100 * time.Millisecond},
		requestEntryInFlight: 7,
		requestStart:         time.Now(),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	scheduler.Wait(t)
	requireResponseHeader(t, localResponseHeaders, "Content-Type", "text/plain")
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected", "abort")
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected-delay", "100ms")
	requireResponseHeaderPresent(t, localResponseHeaders, "x-fault-actual-upstream")
	requireResponseHeaderPresent(t, localResponseHeaders, "x-fault-added-delay")
	requireResponseHeader(t, localResponseHeaders, "x-fault-status", "503")
	requireResponseHeader(t, localResponseHeaders, requestsInFlightHeader, "7")
}

func TestOnResponseHeaders_ImmediateAbort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	activeRequests.Store(99)
	t.Cleanup(func() { activeRequests.Store(0) })

	var localResponseHeaders [][2]string
	handle.EXPECT().SendLocalResponse(
		uint32(500),
		gomock.Any(),
		[]byte("fault filter abort: 500\n"),
		"fault_abort",
	).Do(func(_ uint32, headers [][2]string, _ []byte, _ string) {
		localResponseHeaders = headers
	})

	filter := &latencyFaultFilter{
		handle:               handle,
		matched:              true,
		sample:               fault.ResponseSample{Status: 500, Duration: time.Millisecond},
		requestEntryInFlight: 8,
		requestStart:         time.Now().Add(-10 * time.Millisecond),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected", "abort")
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected-delay", "1ms")
	requireResponseHeaderPresent(t, localResponseHeaders, "x-fault-actual-upstream")
	requireResponseHeaderMissing(t, localResponseHeaders, "x-fault-added-delay")
	requireResponseHeader(t, localResponseHeaders, "x-fault-status", "500")
	requireResponseHeader(t, localResponseHeaders, requestsInFlightHeader, "8")
}

func TestOnResponseHeaders_DelaysExpectedResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	activeRequests.Store(99)
	t.Cleanup(func() { activeRequests.Store(0) })
	scheduler := newResponseTestScheduler()
	handle.EXPECT().GetScheduler().Return(scheduler)
	handle.EXPECT().ContinueResponse()

	filter := &latencyFaultFilter{
		handle:               handle,
		matched:              true,
		sample:               fault.ResponseSample{Status: 200, Duration: 100 * time.Millisecond},
		requestEntryInFlight: 9,
		requestStart:         time.Now(),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, "100ms", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-actual-upstream").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-added-delay").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne("x-fault-status").ToUnsafeString())
	require.Equal(t, "9", headers.GetOne(requestsInFlightHeader).ToUnsafeString())
	scheduler.Wait(t)
}

func TestOnResponseHeaders_ExpectedResponseNeedsNoDelay(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	activeRequests.Store(0)
	t.Cleanup(func() { activeRequests.Store(0) })
	filter := &latencyFaultFilter{
		handle:               handle,
		matched:              true,
		sample:               fault.ResponseSample{Status: 200, Duration: time.Millisecond},
		requestEntryInFlight: 4,
		requestStart:         time.Now().Add(-10 * time.Millisecond),
	}
	filter.startRequest()
	t.Cleanup(filter.OnStreamComplete)
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, "1ms", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-actual-upstream").ToUnsafeString())
	require.Empty(t, headers.GetOne("x-fault-added-delay").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne("x-fault-status").ToUnsafeString())
	require.Equal(t, "4", headers.GetOne(requestsInFlightHeader).ToUnsafeString())
}

func TestOnResponseHeaders_UnconfiguredStatusPassesThrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	activeRequests.Store(99)
	t.Cleanup(func() { activeRequests.Store(0) })
	filter := &latencyFaultFilter{
		handle:               handle,
		matched:              true,
		sample:               fault.ResponseSample{Status: 200, Duration: 100 * time.Millisecond},
		requestEntryInFlight: 6,
		requestStart:         time.Now(),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"404"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, "0s", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-actual-upstream").ToUnsafeString())
	require.Equal(t, "0s", headers.GetOne("x-fault-added-delay").ToUnsafeString())
	require.Equal(t, "404", headers.GetOne("x-fault-status").ToUnsafeString())
	require.Equal(t, "6", headers.GetOne(requestsInFlightHeader).ToUnsafeString())
}

func TestOnResponseHeaders_DiagnosticIncludesWorkerIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	handle.EXPECT().GetWorkerIndex().Return(uint32(3))

	filter := &latencyFaultFilter{
		handle:               handle,
		factory:              &latencyFaultFilterFactory{config: &fault.FilterConfig{Diagnostic: true}},
		matched:              true,
		sample:               fault.ResponseSample{Status: 200, Duration: time.Millisecond},
		requestEntryInFlight: 5,
		requestStart:         time.Now().Add(-10 * time.Millisecond),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, "3", headers.GetOne(workerIndexHeader).ToUnsafeString())
	require.Empty(t, headers.GetOne("x-fault-request-sequence").ToUnsafeString())
}

func TestOnResponseHeaders_NonDiagnosticOmitsWorkerIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)

	filter := &latencyFaultFilter{
		handle:               handle,
		factory:              &latencyFaultFilterFactory{config: &fault.FilterConfig{}},
		matched:              true,
		sample:               fault.ResponseSample{Status: 200, Duration: time.Millisecond},
		requestEntryInFlight: 10,
		requestStart:         time.Now().Add(-10 * time.Millisecond),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Empty(t, headers.GetOne(workerIndexHeader).ToUnsafeString())
	require.Empty(t, headers.GetOne("x-fault-request-sequence").ToUnsafeString())
}

type responseTestScheduler struct {
	done chan struct{}
}

func newResponseTestScheduler() *responseTestScheduler {
	return &responseTestScheduler{done: make(chan struct{})}
}

func (s *responseTestScheduler) Schedule(fn func()) {
	fn()
	close(s.done)
}

func (s *responseTestScheduler) Wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scheduled callback")
	}
}

func findResponseHeader(headers [][2]string, name string) (string, bool) {
	for _, header := range headers {
		if header[0] == name {
			return header[1], true
		}
	}
	return "", false
}

func requireResponseHeader(t *testing.T, headers [][2]string, name, expected string) {
	t.Helper()
	actual, ok := findResponseHeader(headers, name)
	require.True(t, ok, "header %q not found", name)
	require.Equal(t, expected, actual, "header %q", name)
}

func requireResponseHeaderPresent(t *testing.T, headers [][2]string, name string) {
	t.Helper()
	actual, ok := findResponseHeader(headers, name)
	require.True(t, ok, "header %q not found", name)
	require.NotEmpty(t, actual, "header %q", name)
}

func requireResponseHeaderMissing(t *testing.T, headers [][2]string, name string) {
	t.Helper()
	_, ok := findResponseHeader(headers, name)
	require.False(t, ok, "header %q unexpectedly present", name)
}
