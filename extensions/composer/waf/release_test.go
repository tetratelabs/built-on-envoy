// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	fake "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// testScheduler records scheduled tasks so tests decide when (and whether) they run,
// like Envoy running them on a later dispatcher iteration.
type testScheduler struct {
	tasks []func()
}

func (s *testScheduler) Schedule(task func()) { s.tasks = append(s.tasks, task) }

func (s *testScheduler) runAll() {
	tasks := s.tasks
	s.tasks = nil
	for _, t := range tasks {
		t()
	}
}

var releaseTestDirectives = []string{
	"SecRuleEngine On",
	`SecRule REQUEST_HEADERS:x-block "@streq yes" "id:100,phase:1,deny,status:403"`,
	`SecRule REQUEST_HEADERS:x-engine-off "@streq yes" "id:101,phase:1,pass,nolog,ctl:ruleEngine=Off"`,
	`SecRule RESPONSE_HEADERS:x-block "@streq yes" "id:102,phase:3,deny,status:403"`,
}

// newReleaseTestHandle returns a strict mock handle: GetScheduler is only allowed when a
// scheduler is given, and every Log call is counted.
func newReleaseTestHandle(ctrl *gomock.Controller, scheduler *testScheduler, logs *int) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(...any) { *logs++ }).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:1234"), true).AnyTimes()
	if scheduler != nil {
		h.EXPECT().GetScheduler().Return(scheduler)
	}
	return h
}

func upgradeRequestHeaders() shared.HeaderMap {
	return fake.NewFakeHeaderMap(map[string][]string{
		":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/ws"},
		"connection": {"Upgrade"}, "upgrade": {"websocket"},
	})
}

func switchingProtocolsHeaders() shared.HeaderMap {
	return fake.NewFakeHeaderMap(map[string][]string{
		":status": {"101"}, "connection": {"Upgrade"}, "upgrade": {"websocket"},
	})
}

func expectTxMetricsOnce(h *mocks.MockHttpFilterHandle) {
	h.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
	h.EXPECT().RecordHistogramValue(shared.MetricID(3), gomock.Any()).Return(shared.MetricsSuccess)
}

func Test_UpgradeReleasesTransactionAfterHandshake(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(upgradeRequestHeaders(), false))
	require.Empty(t, scheduler.tasks, "the analysis is not done until the response headers")
	require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(switchingProtocolsHeaders(), false))

	// Released off the data path: the transaction is still there until the task runs.
	require.Len(t, scheduler.tasks, 1)
	require.NotNil(t, p.txContext)

	scheduler.runAll()
	require.Nil(t, p.txContext, "transaction must be closed once the handshake is done")
	require.True(t, p.txReleased)

	// Frames and the end of the stream pass through quietly, with no further metrics.
	logs = 0
	frame := fake.NewFakeBodyBuffer([]byte("frame"))
	require.Equal(t, shared.BodyStatusContinue, p.OnRequestBody(frame, false))
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(frame, false))
	require.Equal(t, shared.BodyStatusContinue, p.OnRequestBody(frame, true))
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(frame, true))
	p.OnStreamComplete()
	require.Zero(t, logs, "released streams must not log on every frame")
}

func Test_UpgradeStreamCompletesBeforeScheduledRelease(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	p.OnRequestHeaders(upgradeRequestHeaders(), false)
	p.OnResponseHeaders(switchingProtocolsHeaders(), false)
	require.Len(t, scheduler.tasks, 1)

	// The stream ends before the task runs: OnStreamComplete releases the transaction.
	p.OnStreamComplete()
	require.Nil(t, p.txContext)
	require.False(t, p.txReleased)

	// Envoy drops tasks of completed streams; even if it ran, it must be a no-op.
	scheduler.runAll()
	require.Nil(t, p.txContext)
	require.False(t, p.txReleased)
}

func Test_SSEReleasesTransactionAfterResponseHeaders(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	req := fake.NewFakeHeaderMap(map[string][]string{":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/events"}})
	require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(req, true))
	resp := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}, "content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(resp, false))
	require.True(t, p.isSSE)

	require.Len(t, scheduler.tasks, 1)
	scheduler.runAll()
	require.Nil(t, p.txContext)
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(fake.NewFakeBodyBuffer([]byte("data: x\n\n")), false))
	p.OnStreamComplete()
}

func Test_ShortLivedStreamsAreNotReleasedEarly(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	t.Run("regular request", func(t *testing.T) {
		logs := 0
		h := newReleaseTestHandle(ctrl, nil, &logs) // no GetScheduler call allowed
		expectTxMetricsOnce(h)

		p := factory.Create(h).(*wafPlugin)
		req := fake.NewFakeHeaderMap(map[string][]string{":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/"}})
		p.OnRequestHeaders(req, true)
		p.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}}), true)
		require.NotNil(t, p.txContext, "closed at stream end, as before")
		p.OnStreamComplete()
		require.Nil(t, p.txContext)
	})

	t.Run("interrupted upgrade", func(t *testing.T) {
		logs := 0
		h := newReleaseTestHandle(ctrl, nil, &logs) // no GetScheduler call allowed
		expectTxMetricsOnce(h)
		h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), gomock.Any(), gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess)
		h.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
		h.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), gomock.Any())

		p := factory.Create(h).(*wafPlugin)
		req := fake.NewFakeHeaderMap(map[string][]string{
			":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/ws"},
			"connection": {"Upgrade"}, "upgrade": {"websocket"}, "x-block": {"yes"},
		})
		require.Equal(t, shared.HeadersStatusStop, p.OnRequestHeaders(req, false))
		p.OnStreamComplete()
		require.Nil(t, p.txContext)
	})
}

func Test_UpgradeFramesBeforeScheduledReleaseRuns(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	p.OnRequestHeaders(upgradeRequestHeaders(), false)
	p.OnResponseHeaders(switchingProtocolsHeaders(), false)
	require.Len(t, scheduler.tasks, 1)

	// Envoy runs the task on a later dispatcher iteration: frames can arrive first. They pass
	// through with the transaction still in place and must not schedule another release.
	frame := fake.NewFakeBodyBuffer([]byte("frame"))
	require.Equal(t, shared.BodyStatusContinue, p.OnRequestBody(frame, false))
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(frame, false))
	require.NotNil(t, p.txContext)
	require.Len(t, scheduler.tasks, 1, "a single release per stream")

	scheduler.runAll()
	require.Nil(t, p.txContext)
	require.True(t, p.txReleased)
	p.OnStreamComplete()
}

func Test_UpgradeWithEngineOffIsReleasedAtRequestHeaders(t *testing.T) {
	ctrl := gomock.NewController(t)

	run := func(t *testing.T, factory shared.HttpFilterFactory, headers shared.HeaderMap) {
		scheduler := &testScheduler{}
		logs := 0
		h := newReleaseTestHandle(ctrl, scheduler, &logs) // no tx metrics with the engine off

		p := factory.Create(h).(*wafPlugin)
		require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(headers, false))
		require.Len(t, scheduler.tasks, 1, "nothing left to analyze: release right after the request headers")

		scheduler.runAll()
		require.Nil(t, p.txContext)

		logs = 0
		require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(switchingProtocolsHeaders(), false))
		require.Equal(t, shared.BodyStatusContinue, p.OnRequestBody(fake.NewFakeBodyBuffer([]byte("frame")), false))
		p.OnStreamComplete()
		require.Zero(t, logs)
	}

	t.Run("SecRuleEngine Off", func(t *testing.T) {
		run(t, newWAFFactory(t, ctrl, []string{"SecRuleEngine Off"}, "FULL"), upgradeRequestHeaders())
	})
	t.Run("ctl:ruleEngine=Off", func(t *testing.T) {
		headers := upgradeRequestHeaders()
		headers.Set("x-engine-off", "yes")
		run(t, newWAFFactory(t, ctrl, releaseTestDirectives, "FULL"), headers)
	})
}

func Test_RequestOnlyUpgradeAfterRelease(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "REQUEST_ONLY")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(upgradeRequestHeaders(), false))
	require.Len(t, scheduler.tasks, 1, "phase 2 ends the analysis in request-only mode")
	scheduler.runAll()
	require.Nil(t, p.txContext)

	// The response arrives after the release: it passes through quietly.
	logs = 0
	require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(switchingProtocolsHeaders(), false))
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(fake.NewFakeBodyBuffer([]byte("frame")), false))
	require.Equal(t, shared.TrailersStatusContinue, p.OnResponseTrailers(fake.NewFakeHeaderMap(nil)))
	p.OnStreamComplete()
	require.Zero(t, logs)
}

func Test_UpgradeRejectedByUpstreamIsReleased(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	p.OnRequestHeaders(upgradeRequestHeaders(), false)
	// The upstream refuses the upgrade: the response body is not inspected for upgrade
	// requests, so the analysis is done at the response headers as for a 101.
	resp := fake.NewFakeHeaderMap(map[string][]string{":status": {"401"}, "content-type": {"text/plain"}})
	require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(resp, false))
	require.Len(t, scheduler.tasks, 1)
	scheduler.runAll()

	logs = 0
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(fake.NewFakeBodyBuffer([]byte("unauthorized")), true))
	p.OnStreamComplete()
	require.Zero(t, logs)
}

func Test_TrailersAfterRelease(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	scheduler := &testScheduler{}
	logs := 0
	h := newReleaseTestHandle(ctrl, scheduler, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	p.OnRequestHeaders(upgradeRequestHeaders(), false)
	p.OnResponseHeaders(switchingProtocolsHeaders(), false)
	scheduler.runAll()

	logs = 0
	require.Equal(t, shared.TrailersStatusContinue, p.OnRequestTrailers(fake.NewFakeHeaderMap(nil)))
	require.Equal(t, shared.TrailersStatusContinue, p.OnResponseTrailers(fake.NewFakeHeaderMap(nil)))
	p.OnStreamComplete()
	require.Zero(t, logs)
}

func Test_UpgradeBlockedInResponsePhaseIsNotReleasedEarly(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	logs := 0
	h := newReleaseTestHandle(ctrl, nil, &logs) // no GetScheduler call allowed
	expectTxMetricsOnce(h)
	h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), gomock.Any(), gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess)
	h.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
	h.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), gomock.Any())

	p := factory.Create(h).(*wafPlugin)
	require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(upgradeRequestHeaders(), false))
	resp := fake.NewFakeHeaderMap(map[string][]string{
		":status": {"101"}, "connection": {"Upgrade"}, "upgrade": {"websocket"}, "x-block": {"yes"},
	})
	require.Equal(t, shared.HeadersStatusStop, p.OnResponseHeaders(resp, false))
	p.OnStreamComplete()
	require.Nil(t, p.txContext)
}
