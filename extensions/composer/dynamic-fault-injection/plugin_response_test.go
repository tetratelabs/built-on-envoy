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

func TestOnResponseHeaders_DelayedAbort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
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
		handle:       handle,
		matched:      true,
		sample:       fault.ResponseSample{Status: 503, Duration: 100 * time.Millisecond},
		requestStart: time.Now(),
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
}

func TestOnResponseHeaders_ImmediateAbort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)

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
		handle:       handle,
		matched:      true,
		sample:       fault.ResponseSample{Status: 500, Duration: time.Millisecond},
		requestStart: time.Now().Add(-10 * time.Millisecond),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected", "abort")
	requireResponseHeader(t, localResponseHeaders, "x-fault-injected-delay", "1ms")
	requireResponseHeaderPresent(t, localResponseHeaders, "x-fault-actual-upstream")
	requireResponseHeaderMissing(t, localResponseHeaders, "x-fault-added-delay")
	requireResponseHeader(t, localResponseHeaders, "x-fault-status", "500")
}

func TestOnResponseHeaders_DelaysExpectedResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	scheduler := newResponseTestScheduler()
	handle.EXPECT().GetScheduler().Return(scheduler)
	handle.EXPECT().ContinueResponse()

	filter := &latencyFaultFilter{
		handle:       handle,
		matched:      true,
		sample:       fault.ResponseSample{Status: 200, Duration: 100 * time.Millisecond},
		requestStart: time.Now(),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, "100ms", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-actual-upstream").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-added-delay").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne("x-fault-status").ToUnsafeString())
	scheduler.Wait(t)
}

func TestOnResponseHeaders_ExpectedResponseNeedsNoDelay(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	filter := &latencyFaultFilter{
		handle:       handle,
		matched:      true,
		sample:       fault.ResponseSample{Status: 200, Duration: time.Millisecond},
		requestStart: time.Now().Add(-10 * time.Millisecond),
	}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}})

	status := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, "1ms", headers.GetOne("x-fault-injected-delay").ToUnsafeString())
	require.NotEmpty(t, headers.GetOne("x-fault-actual-upstream").ToUnsafeString())
	require.Empty(t, headers.GetOne("x-fault-added-delay").ToUnsafeString())
	require.Equal(t, "200", headers.GetOne("x-fault-status").ToUnsafeString())
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
