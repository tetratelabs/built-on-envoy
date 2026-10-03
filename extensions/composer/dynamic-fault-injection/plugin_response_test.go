// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These tests cover response headers and local replies because both paths publish fault metadata.
package impl

import (
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/config"
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
	require.Equal(t, attrs.UpstreamStatus, headers.GetOne(upstreamStatusHeader).ToUnsafeString())
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
	requireResponseHeader(t, headers, upstreamStatusHeader, attrs.UpstreamStatus)
	requireResponseHeader(t, headers, requestsInFlightHeader, "7")
	requireResponseHeader(t, headers, injectedHeader, attrs.Injected)
	requireResponseHeader(t, headers, workerIndexHeader, attrs.WorkerIndex)
	requireHeaderCount(t, headers, injectedDelayHeader, 1)
	requireHeaderCount(t, headers, actualUpstreamHeader, 1)
	requireHeaderCount(t, headers, addedDelayHeader, 1)
	requireHeaderCount(t, headers, statusHeader, 1)
	requireHeaderCount(t, headers, upstreamStatusHeader, 1)
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
		UpstreamStatus:   "200",
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
	span.EXPECT().SetTag(upstreamStatusTag, "200").Times(1)
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

func requireResponseTimingHeader(t *testing.T, headers [][2]string, name string) {
	t.Helper()
	values := headerValues(headers, name)
	require.Len(t, values, 1, "header %s", name)
	require.Regexp(t, `^[0-9]+\.[0-9]{3}ms$`, values[0])
}

func TestOnResponseHeaders_TimingHeaderPrecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "zero", want: "0.000ms"},
		{name: "nanoseconds", duration: 1, want: "0.000ms"},
		{name: "microseconds", duration: time.Microsecond, want: "0.001ms"},
		{name: "round down", duration: 1234499 * time.Nanosecond, want: "1.234ms"},
		{name: "round up", duration: 1234567 * time.Nanosecond, want: "1.235ms"},
		{name: "carry", duration: 999999600 * time.Nanosecond, want: "1000.000ms"},
		{name: "seconds", duration: 1234567890 * time.Nanosecond, want: "1234.568ms"},
		{name: "minutes", duration: time.Minute + 123456789*time.Nanosecond, want: "60123.457ms"},
		{name: "hours", duration: time.Hour + 123456789*time.Nanosecond, want: "3600123.457ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			handle := mocks.NewMockHttpFilterHandle(ctrl)
			span := mocks.NewMockSpan(ctrl)
			handle.EXPECT().GetActiveSpan().Return(span).Times(1)
			tags := make(map[string]string)
			span.EXPECT().SetTag(gomock.Any(), gomock.Any()).Do(func(name, value string) {
				tags[name] = value
			}).AnyTimes()
			filter := &latencyFaultFilter{
				handle:       handle,
				matched:      true,
				sample:       fault.ResponseSample{Status: 200, Duration: tc.duration},
				requestStart: time.Now().Add(-tc.duration - time.Second),
			}
			headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

			require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(headers, false))
			require.Equal(t, tc.want, headers.GetOne(injectedDelayHeader).ToUnsafeString())
			upstream := headers.GetOne(actualUpstreamHeader).ToUnsafeString()
			require.Regexp(t, `^[0-9]+\.[0-9]{3}ms$`, upstream)
			parsed, err := time.ParseDuration(upstream)
			require.NoError(t, err)
			require.GreaterOrEqual(t, parsed+time.Microsecond, tc.duration+time.Second)
			require.Equal(t, tc.want, tags[injectedDelayTag])
			require.Equal(t, upstream, tags[actualUpstreamTag])
		})
	}
}

func TestOnResponseHeaders_DelayedAbort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	activeRequests.Store(99)
	t.Cleanup(func() { activeRequests.Store(0) })
	scheduler := newResponseTestScheduler()
	handle.EXPECT().GetScheduler().Return(scheduler)
	responseBody := `{"type":"about:blank","title":"Unavailable"}`

	var localResponseHeaders [][2]string
	handle.EXPECT().SendLocalResponse(
		uint32(503),
		gomock.Any(),
		[]byte(responseBody),
		"fault_abort",
	).Do(func(_ uint32, headers [][2]string, _ []byte, _ string) {
		localResponseHeaders = headers
	})

	filter := &latencyFaultFilter{
		handle:  handle,
		matched: true,
		sample: fault.ResponseSample{Status: 503, Duration: 100 * time.Millisecond, LocalResponse: &config.LocalResponseConfig{
			Body: &responseBody,
			Headers: []config.LocalResponseHeader{
				{Name: "Content-Type", Value: "application/problem+json"},
				{Name: "Retry-After", Value: "2"},
			},
		}},
		requestEntryInFlight: 7,
		requestStart:         time.Now(),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	scheduler.Wait(t)
	requireResponseHeader(t, localResponseHeaders, "content-type", "application/problem+json")
	requireResponseHeader(t, localResponseHeaders, "retry-after", "2")
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected", "abort")
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected-delay", "100.000ms")
	requireResponseTimingHeader(t, localResponseHeaders, actualUpstreamHeader)
	requireResponseTimingHeader(t, localResponseHeaders, addedDelayHeader)
	requireResponseHeader(t, localResponseHeaders, "x-fault-status", "503")
	requireResponseHeader(t, localResponseHeaders, upstreamStatusHeader, "200")
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
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected-delay", "1.000ms")
	requireResponseTimingHeader(t, localResponseHeaders, actualUpstreamHeader)
	requireResponseHeaderMissing(t, localResponseHeaders, "x-fault-added-delay")
	requireResponseHeader(t, localResponseHeaders, "x-fault-status", "500")
	requireResponseHeader(t, localResponseHeaders, upstreamStatusHeader, "200")
	requireResponseHeader(t, localResponseHeaders, requestsInFlightHeader, "8")
}

func TestOnResponseHeaders_ExplicitEmptyErrorBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	empty := ""
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), []byte{}, "fault_abort")
	filter := &latencyFaultFilter{
		handle:  handle,
		matched: true,
		sample: fault.ResponseSample{Status: 503, Duration: time.Millisecond, LocalResponse: &config.LocalResponseConfig{
			Body: &empty,
		}},
		requestStart: time.Now().Add(-10 * time.Millisecond),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
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
	require.Equal(t, "100.000ms", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.Regexp(t, `^[0-9]+\.[0-9]{3}ms$`, headers.GetOne(actualUpstreamHeader).ToUnsafeString())
	require.Regexp(t, `^[0-9]+\.[0-9]{3}ms$`, headers.GetOne(addedDelayHeader).ToUnsafeString())
	require.Equal(t, "200", headers.GetOne("x-fault-status").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne(upstreamStatusHeader).ToUnsafeString())
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
	require.Equal(t, "1.000ms", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.Regexp(t, `^[0-9]+\.[0-9]{3}ms$`, headers.GetOne(actualUpstreamHeader).ToUnsafeString())
	require.Empty(t, headers.GetOne("x-fault-added-delay").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne("x-fault-status").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne(upstreamStatusHeader).ToUnsafeString())
	require.Equal(t, "4", headers.GetOne(requestsInFlightHeader).ToUnsafeString())
}

func TestOnResponseHeaders_SampledSuccessOverridesUpstreamError(t *testing.T) {
	for _, tc := range []struct {
		name         string
		duration     time.Duration
		upstreamTime time.Duration
		wantStatus   shared.HeadersStatus
		wantTiming   string
	}{
		{name: "immediate", duration: time.Millisecond, upstreamTime: 10 * time.Millisecond, wantStatus: shared.HeadersStatusStop, wantTiming: "1.000ms"},
		{name: "delayed", duration: 100 * time.Millisecond, wantStatus: shared.HeadersStatusStopAllAndBuffer, wantTiming: "100.000ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			handle := mocks.NewMockHttpFilterHandle(ctrl)
			span := mocks.NewMockSpan(ctrl)
			handle.EXPECT().GetActiveSpan().Return(span).Times(1)
			tags := make(map[string]string)
			span.EXPECT().SetTag(gomock.Any(), gomock.Any()).Do(func(name, value string) { tags[name] = value }).AnyTimes()
			body := `{"error":"unavailable"}`
			localResponse := &config.LocalResponseConfig{
				Body: &body,
				Headers: []config.LocalResponseHeader{
					{Name: "Content-Type", Value: "application/json"},
					{Name: "Set-Cookie", Value: "first=1"},
					{Name: "Set-Cookie", Value: "second=2"},
				},
			}
			var gotHeaders [][2]string
			handle.EXPECT().SendLocalResponse(uint32(200), gomock.Any(), []byte(body), "fault_response").Do(
				func(_ uint32, headers [][2]string, _ []byte, _ string) { gotHeaders = headers },
			)
			scheduler := newResponseTestScheduler()
			if tc.wantStatus == shared.HeadersStatusStopAllAndBuffer {
				handle.EXPECT().GetScheduler().Return(scheduler)
			}
			filter := &latencyFaultFilter{
				handle:               handle,
				matched:              true,
				sample:               fault.ResponseSample{Status: 200, Duration: tc.duration, LocalResponse: localResponse},
				requestEntryInFlight: 6,
				requestStart:         time.Now().Add(-tc.upstreamTime),
			}
			headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"500"}})

			status := filter.OnResponseHeaders(headers, false)
			require.Equal(t, tc.wantStatus, status)
			if status == shared.HeadersStatusStopAllAndBuffer {
				scheduler.Wait(t)
			}
			requireResponseHeader(t, gotHeaders, "content-type", "application/json")
			requireResponseHeader(t, gotHeaders, injectedHeader, "response")
			requireResponseHeader(t, gotHeaders, injectedDelayHeader, tc.wantTiming)
			requireResponseHeader(t, gotHeaders, statusHeader, "200")
			requireResponseHeader(t, gotHeaders, upstreamStatusHeader, "500")
			requireResponseHeader(t, gotHeaders, requestsInFlightHeader, "6")
			requireResponseHeaderPresent(t, gotHeaders, actualUpstreamHeader)
			requireHeaderCount(t, gotHeaders, "set-cookie", 2)
			require.Equal(t, []string{"first=1", "second=2"}, headerValues(gotHeaders, "set-cookie"))
			require.Equal(t, "200", tags[statusTag])
			require.Equal(t, "500", tags[upstreamStatusTag])
		})
	}
}

func headerValues(headers [][2]string, name string) []string {
	values := make([]string, 0)
	for _, header := range headers {
		if header[0] == name {
			values = append(values, header[1])
		}
	}
	return values
}

func TestOnResponseHeaders_DelaysAndForcesBodylessSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	scheduler := newResponseTestScheduler()
	handle.EXPECT().GetScheduler().Return(scheduler)
	handle.EXPECT().SendLocalResponse(uint32(204), gomock.Any(), []byte{}, "fault_response")
	filter := &latencyFaultFilter{
		handle:       handle,
		matched:      true,
		sample:       fault.ResponseSample{Status: 204, Duration: 60 * time.Millisecond},
		requestStart: time.Now(),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, filter.OnResponseHeaders(headers, false))
	scheduler.Wait(t)
}

func TestOnResponseHeaders_DiagnosticIncludesWorkerIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	handle.EXPECT().GetWorkerIndex().Return(uint32(3))

	filter := &latencyFaultFilter{
		handle:               handle,
		factory:              &latencyFaultFilterFactory{config: &config.FilterConfig{Diagnostic: true}},
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
		factory:              &latencyFaultFilterFactory{config: &config.FilterConfig{}},
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
