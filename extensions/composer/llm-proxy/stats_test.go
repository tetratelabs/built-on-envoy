// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newTestStats builds a real llmProxyStats backed by a mock config handle.
// The handle assigns MetricIDs 1–7 sequentially (counters first, then histograms)
// so individual metrics can be identified in EXPECT calls.
func newTestStats(ctrl *gomock.Controller) llmProxyStats {
	cfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	id := shared.MetricID(0)
	nextID := func(_ string, _ ...string) (shared.MetricID, shared.MetricsResult) {
		id++
		return id, shared.MetricsSuccess
	}
	cfgHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(5)
	cfgHandle.EXPECT().DefineHistogram(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(2)
	return newLLMProxyStats(cfgHandle)
}

// MetricIDs assigned by newTestStats in definition order (must match newLLMProxyStats).
const (
	idRequestTotal shared.MetricID = 1
	idRequestError shared.MetricID = 2
	idInputTokens  shared.MetricID = 3
	idOutputTokens shared.MetricID = 4
	idTotalTokens  shared.MetricID = 5
	idTTFT         shared.MetricID = 6
	idTPOT         shared.MetricID = 7
)

// defaultCfgWithStats returns a copy of defaultCfg with the provided stats installed.
func defaultCfgWithStats(s llmProxyStats) *llmProxyConfig {
	cfg := defaultCfg()
	cfg.stats = s
	return cfg
}

// --- newLLMProxyStats ---

func TestNewLLMProxyStats_AllMetricsDefined(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)
	require.NotEqual(t, shared.MetricID(0), s.requestTotal)
	require.NotEqual(t, shared.MetricID(0), s.requestError)
	require.NotEqual(t, shared.MetricID(0), s.inputTokens)
	require.NotEqual(t, shared.MetricID(0), s.outputTokens)
	require.NotEqual(t, shared.MetricID(0), s.totalTokens)
	require.NotEqual(t, shared.MetricID(0), s.requestTTFT)
	require.NotEqual(t, shared.MetricID(0), s.requestTPOT)
}

// --- filter integration tests ---

func TestStats_RequestBody_Success_RecordsTotal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)

	body := []byte(`{"model":"gpt-4o","stream":false}`)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle: handle, config: defaultCfgWithStats(s),
		matched: true, kind: KindOpenAI, factory: &openaiFactory{},
	}
	filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
	require.False(t, filter.requestSentAt.IsZero(), "requestSentAt must be set on success")
}

// TestStats_RequestBody_ParseError_RecordsErrorAndTotal verifies that a parse
// failure increments both the error counter and the request-total counter.
// The request-total is always incremented (even on error) when requestSentAt
// has not yet been set, so that metrics are never under-counted.
func TestStats_RequestBody_ParseError_RecordsErrorAndTotal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)

	body := []byte(`{bad json}`)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).AnyTimes()
	// requestSentAt is zero when the error occurs → both error and total are incremented.
	handle.EXPECT().IncrementCounterValue(idRequestError, uint64(1), "openai", "").
		Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "").
		Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle: handle, config: defaultCfgWithStats(s),
		matched: true, kind: KindOpenAI, factory: &openaiFactory{},
	}
	filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
	require.True(t, filter.hasError)
}

func TestStats_NonStreamingResponse_RecordsTokenCounters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)

	body := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	// requestSentAt is zero → no TTFT/TPOT histograms recorded.

	filter := &llmProxyFilter{
		handle: handle, config: defaultCfgWithStats(s),
		matched: true, kind: KindOpenAI, factory: &openaiFactory{},
		model: "gpt-4o",
	}
	filter.OnResponseBody(fake.NewFakeBodyBuffer(body), true)
}

func TestStats_NonStreamingResponse_RecordsTTFT_TPOT_WhenRequestSentAtSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)

	body := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idTTFT, gomock.Any(), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idTPOT, gomock.Any(), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle:        handle,
		config:        defaultCfgWithStats(s),
		matched:       true,
		kind:          KindOpenAI,
		factory:       &openaiFactory{},
		model:         "gpt-4o",
		requestSentAt: time.Now().Add(-100 * time.Millisecond),
	}
	filter.OnResponseBody(fake.NewFakeBodyBuffer(body), true)
	require.False(t, filter.firstChunkAt.IsZero(), "firstChunkAt must be set for non-streaming responses")
}

func TestStats_StreamingResponse_RecordsTTFT_TPOT_Tokens(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(8), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(4), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(12), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	// TTFT and TPOT values depend on wall-clock timing; match any uint64 value.
	handle.EXPECT().RecordHistogramValue(idTTFT, gomock.Any(), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idTPOT, gomock.Any(), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	acc := newOpenAISSEParser()
	filter := &llmProxyFilter{
		handle: handle, config: defaultCfgWithStats(s),
		matched: true, kind: KindOpenAI, factory: &openaiFactory{},
		model:         "gpt-4o",
		sseParser:     acc,
		requestSentAt: time.Now().Add(-100 * time.Millisecond), // simulate sent 100 ms ago
	}

	chunk := fake.NewFakeBodyBuffer([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n"))
	done := fake.NewFakeBodyBuffer([]byte("data: [DONE]\n"))

	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(chunk, false))
	require.False(t, filter.firstChunkAt.IsZero(), "firstChunkAt must be set on first text token chunk")
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(done, true))
}

func TestStats_StreamingResponse_NoTTFT_WhenRequestSentAtNotSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := newTestStats(ctrl)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	// Token counters still recorded; TTFT/TPOT not recorded because requestSentAt is zero.
	handle.EXPECT().IncrementCounterValue(idInputTokens, gomock.Any(), gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idOutputTokens, gomock.Any(), gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idTotalTokens, gomock.Any(), gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	// No RecordHistogramValue expected.

	acc := newOpenAISSEParser()
	filter := &llmProxyFilter{
		handle: handle, config: defaultCfgWithStats(s),
		matched: true, kind: KindOpenAI, factory: &openaiFactory{},
		model:     "gpt-4o",
		sseParser: acc,
		// requestSentAt left as zero value → TTFT/TPOT skipped
	}

	chunk := fake.NewFakeBodyBuffer([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n"))
	done := fake.NewFakeBodyBuffer([]byte("data: [DONE]\n"))
	filter.OnResponseBody(chunk, false)
	filter.OnResponseBody(done, true)
}
