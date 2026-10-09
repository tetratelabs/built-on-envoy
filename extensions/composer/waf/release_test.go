// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	fake "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

var releaseTestDirectives = []string{
	"SecRuleEngine On",
	`SecRule REQUEST_HEADERS:x-block "@streq yes" "id:100,phase:1,deny,status:403"`,
	`SecRule REQUEST_HEADERS:x-engine-off "@streq yes" "id:101,phase:1,pass,nolog,ctl:ruleEngine=Off"`,
}

// newReleaseTestHandle returns a strict mock handle (any call not expected by the test fails
// it) that counts every Log call.
func newReleaseTestHandle(ctrl *gomock.Controller, logs *int) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(...any) { *logs++ }).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:1234"), true).AnyTimes()
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

	logs := 0
	h := newReleaseTestHandle(ctrl, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(upgradeRequestHeaders(), false))
	require.NotNil(t, p.txContext, "the analysis is not done until the response headers")
	require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(switchingProtocolsHeaders(), false))

	require.Nil(t, p.txContext, "transaction must be closed once the handshake is done")
	require.True(t, p.txReleased)

	// Everything after the release passes through quietly, with no further metrics.
	logs = 0
	frame := fake.NewFakeBodyBuffer([]byte("frame"))
	require.Equal(t, shared.BodyStatusContinue, p.OnRequestBody(frame, false))
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(frame, false))
	require.Equal(t, shared.TrailersStatusContinue, p.OnRequestTrailers(fake.NewFakeHeaderMap(nil)))
	require.Equal(t, shared.TrailersStatusContinue, p.OnResponseTrailers(fake.NewFakeHeaderMap(nil)))
	p.OnStreamComplete()
	require.Zero(t, logs, "released streams must not log on every frame")
}

func Test_UpgradeAuditLogIsWrittenAtReleaseOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	auditLog := filepath.Join(t.TempDir(), "audit.log")
	factory := newWAFFactory(t, ctrl, append([]string{
		"SecAuditEngine On", "SecAuditLogType Serial", "SecAuditLogFormat JSON", "SecAuditLog " + auditLog,
	}, releaseTestDirectives...), "FULL")
	entries := func() int {
		content, err := os.ReadFile(filepath.Clean(auditLog))
		require.NoError(t, err)
		return bytes.Count(content, []byte("\n"))
	}

	logs := 0
	h := newReleaseTestHandle(ctrl, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	p.OnRequestHeaders(upgradeRequestHeaders(), false)
	require.Zero(t, entries())
	p.OnResponseHeaders(switchingProtocolsHeaders(), false)
	require.Equal(t, 1, entries(), "the audit log is written when the transaction is released")

	p.OnRequestBody(fake.NewFakeBodyBuffer([]byte("frame")), false)
	p.OnStreamComplete()
	require.Equal(t, 1, entries(), "the transaction must not be logged again at stream end")
}

func Test_SSEReleasesTransactionAfterResponseHeaders(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	logs := 0
	h := newReleaseTestHandle(ctrl, &logs)
	expectTxMetricsOnce(h)

	p := factory.Create(h).(*wafPlugin)
	req := fake.NewFakeHeaderMap(map[string][]string{":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/events"}})
	require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(req, true))
	resp := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}, "content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(resp, false))
	require.True(t, p.isSSE)
	require.Nil(t, p.txContext)

	logs = 0
	require.Equal(t, shared.BodyStatusContinue, p.OnResponseBody(fake.NewFakeBodyBuffer([]byte("data: x\n\n")), false))
	p.OnStreamComplete()
	require.Zero(t, logs)
}

func Test_StreamsNotReleasedEarly(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := newWAFFactory(t, ctrl, releaseTestDirectives, "FULL")

	t.Run("regular request", func(t *testing.T) {
		logs := 0
		h := newReleaseTestHandle(ctrl, &logs)
		expectTxMetricsOnce(h)

		p := factory.Create(h).(*wafPlugin)
		req := fake.NewFakeHeaderMap(map[string][]string{":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/"}})
		p.OnRequestHeaders(req, true)
		p.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}}), true)
		require.NotNil(t, p.txContext, "closed at stream end, as before")
		require.False(t, p.txReleased)
		p.OnStreamComplete()
		require.Nil(t, p.txContext)
	})

	t.Run("blocked upgrade", func(t *testing.T) {
		logs := 0
		h := newReleaseTestHandle(ctrl, &logs)
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
		require.NotNil(t, p.txContext, "an interrupted stream is released by OnStreamComplete")
		require.False(t, p.txReleased)
		p.OnStreamComplete()
		require.Nil(t, p.txContext)
	})
}

// In these cases the analysis is already done after the request headers, so the transaction
// is released before the response arrives.
func Test_UpgradeReleasedAtRequestHeaders(t *testing.T) {
	ctrl := gomock.NewController(t)

	tests := []struct {
		name      string
		mode      string
		header    string
		txMetrics bool
	}{
		{name: "request-only mode", mode: "REQUEST_ONLY", txMetrics: true},
		// No transaction metrics are recorded with the engine off.
		{name: "ctl:ruleEngine=Off", mode: "FULL", header: "x-engine-off"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := newWAFFactory(t, ctrl, releaseTestDirectives, tc.mode)
			logs := 0
			h := newReleaseTestHandle(ctrl, &logs)
			if tc.txMetrics {
				expectTxMetricsOnce(h)
			}
			headers := upgradeRequestHeaders()
			if tc.header != "" {
				headers.Set(tc.header, "yes")
			}

			p := factory.Create(h).(*wafPlugin)
			require.Equal(t, shared.HeadersStatusContinue, p.OnRequestHeaders(headers, false))
			require.Nil(t, p.txContext)
			require.True(t, p.txReleased)

			// The response arrives after the release: it passes through quietly.
			logs = 0
			require.Equal(t, shared.HeadersStatusContinue, p.OnResponseHeaders(switchingProtocolsHeaders(), false))
			p.OnStreamComplete()
			require.Zero(t, logs)
		})
	}
}

func Test_ReleaseTransactionIsNoOpWithoutTransaction(t *testing.T) {
	p := &wafPlugin{} // no handle: any use of it would panic
	require.NotPanics(t, p.releaseTransaction)
	require.Nil(t, p.txContext)
}
