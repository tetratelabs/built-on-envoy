// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openfga

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func testConfig(t *testing.T) *parsedConfig {
	t.Helper()
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_access"},
		Object:      valueSource{Header: "x-resource", Prefix: "document:"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	parsed, err := parseConfig(data)
	require.NoError(t, err)
	return parsed
}

func testMultiRuleConfig(t *testing.T) *parsedConfig {
	t.Helper()
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Rules: []checkRule{
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-ai-eg-model": "*"}},
				Relation: valueSource{Value: "can_use"},
				Object:   valueSource{Header: "x-ai-eg-model", Prefix: "model:"},
			},
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-mcp-tool": "*"}},
				Relation: valueSource{Value: "can_invoke"},
				Object:   valueSource{Header: "x-mcp-tool", Prefix: "tool:"},
			},
			{
				Relation: valueSource{Value: "can_access"},
				Object:   valueSource{Header: "x-resource-id", Prefix: "resource:"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	parsed, err := parseConfig(data)
	require.NoError(t, err)
	return parsed
}

// testMultiRuleNoCatchAllConfig returns a config with only header-matched rules (no catch-all).
func testMultiRuleNoCatchAllConfig(t *testing.T) *parsedConfig {
	t.Helper()
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Rules: []checkRule{
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-ai-eg-model": "*"}},
				Relation: valueSource{Value: "can_use"},
				Object:   valueSource{Header: "x-ai-eg-model", Prefix: "model:"},
			},
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-mcp-tool": "*"}},
				Relation: valueSource{Value: "can_invoke"},
				Object:   valueSource{Header: "x-mcp-tool", Prefix: "tool:"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	parsed, err := parseConfig(data)
	require.NoError(t, err)
	return parsed
}

func testMetrics() *openfgaMetrics {
	return &openfgaMetrics{
		requestsTotal: shared.MetricID(1),
		enabled:       true,
	}
}

func newMockFilterHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return mockHandle
}

func TestOnRequestHeaders(t *testing.T) {
	t.Run("catch-all rule matches any request", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(f.metrics.requestsTotal, uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		tk := body["tuple_key"].(map[string]any)
		require.Equal(t, "user:alice", tk["user"])
		require.Equal(t, "can_access", tk["relation"])
		require.Equal(t, "document:planning", tk["object"])
	})

	t.Run("no matching rule with fail_open=false sends 403", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), gomock.Any())
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDenied).Return(shared.MetricsSuccess)

		cfg := testMultiRuleNoCatchAllConfig(t)
		cfg.failOpen = false
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id": {"alice"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("no matching rule with fail_open=true continues", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionFailOpen).Return(shared.MetricsSuccess)

		cfg := testMultiRuleNoCatchAllConfig(t)
		cfg.failOpen = true
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id": {"alice"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("missing parameters with fail_open=false sends 403", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), gomock.Any())
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDenied).Return(shared.MetricsSuccess)

		cfg := testConfig(t)
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id": {"alice"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("missing parameters with fail_open=true continues", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionFailOpen).Return(shared.MetricsSuccess)

		cfg := testConfig(t)
		cfg.failOpen = true
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id": {"alice"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("callout parameters are correct", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedCluster string
		var capturedHeaders [][2]string
		var capturedBody []byte
		var capturedTimeout uint64
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(cluster string, headers [][2]string, body []byte, timeout uint64, _ shared.HttpCalloutCallback) {
			capturedCluster = cluster
			capturedHeaders = headers
			capturedBody = body
			capturedTimeout = timeout
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(f.metrics.requestsTotal, uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
		})
		_ = f.OnRequestHeaders(headers, false)

		require.Equal(t, "openfga", capturedCluster)
		require.Equal(t, uint64(5000), capturedTimeout)
		require.Equal(t, "POST", headerValue(capturedHeaders, ":method"))
		require.Equal(t, "/stores/store1/check", headerValue(capturedHeaders, ":path"))
		require.Equal(t, "openfga:8080", headerValue(capturedHeaders, "host"))
		require.Equal(t, "application/json", headerValue(capturedHeaders, "content-type"))

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		tk := body["tuple_key"].(map[string]any)
		require.Equal(t, "user:alice", tk["user"])
		_, hasConsistency := body["consistency"]
		require.False(t, hasConsistency)
	})

	t.Run("callout body includes consistency when configured", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfgJSON := openfgaConfig{
			Cluster:     "openfga",
			OpenFGAHost: "openfga:8080",
			StoreID:     "store1",
			Consistency: "HIGHER_CONSISTENCY",
			User:        valueSource{Header: "x-user-id", Prefix: "user:"},
			Relation:    valueSource{Value: "can_access"},
			Object:      valueSource{Header: "x-resource", Prefix: "document:"},
		}
		raw, err := json.Marshal(cfgJSON)
		require.NoError(t, err)
		parsedCfg, err := parseConfig(raw)
		require.NoError(t, err)

		f := &openfgaFilter{handle: mockHandle, config: parsedCfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(f.metrics.requestsTotal, uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
		})
		_ = f.OnRequestHeaders(headers, false)

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		require.Equal(t, "HIGHER_CONSISTENCY", body["consistency"])
	})

	t.Run("callout init failure with fail_open=false sends 502", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), gomock.Any())
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionError).Return(shared.MetricsSuccess)

		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(shared.HttpCalloutInitClusterNotFound, uint64(0))

		cfg := testConfig(t)
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("callout init failure with fail_open=true continues", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionFailOpen).Return(shared.MetricsSuccess)

		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(shared.HttpCalloutInitClusterNotFound, uint64(0))

		cfg := testConfig(t)
		cfg.failOpen = true
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("multi-rule selection", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testMultiRuleConfig(t)
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		tests := []struct {
			name     string
			headers  map[string][]string
			wantUser string
			wantRel  string
			wantObj  string
		}{
			{
				name:     "AI rule",
				headers:  map[string][]string{"x-user-id": {"alice"}, "x-ai-eg-model": {"gpt-4"}},
				wantUser: "user:alice",
				wantRel:  "can_use",
				wantObj:  "model:gpt-4",
			},
			{
				name:     "MCP rule",
				headers:  map[string][]string{"x-user-id": {"alice"}, "x-mcp-tool": {"github__issue_read"}},
				wantUser: "user:alice",
				wantRel:  "can_invoke",
				wantObj:  "tool:github__issue_read",
			},
			{
				name:     "catch-all rule",
				headers:  map[string][]string{"x-user-id": {"alice"}, "x-resource-id": {"planning"}},
				wantUser: "user:alice",
				wantRel:  "can_access",
				wantObj:  "resource:planning",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var capturedBody []byte
				mockHandle.EXPECT().HttpCallout(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
					capturedBody = body
				}).Return(shared.HttpCalloutInitSuccess, uint64(1))
				mockHandle.EXPECT().IncrementCounterValue(f.metrics.requestsTotal, uint64(1), gomock.Any()).AnyTimes()

				status := f.OnRequestHeaders(fake.NewFakeHeaderMap(tt.headers), false)
				require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

				var body map[string]any
				require.NoError(t, json.Unmarshal(capturedBody, &body))
				tk := body["tuple_key"].(map[string]any)
				require.Equal(t, tt.wantUser, tk["user"])
				require.Equal(t, tt.wantRel, tk["relation"])
				require.Equal(t, tt.wantObj, tk["object"])
			})
		}
	})
}

func TestCallback(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().ContinueRequest()
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionAllowed).Return(shared.MetricsSuccess)

		cb := &openfgaCallback{handle: mockHandle, config: testConfig(t), metrics: testMetrics()}
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":true}`)},
		)
	})

	t.Run("denied", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), gomock.Any())
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDenied).Return(shared.MetricsSuccess)

		cb := &openfgaCallback{handle: mockHandle, config: testConfig(t), metrics: testMetrics()}
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":false}`)},
		)
	})

	t.Run("denied with dry_run continues", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().ContinueRequest()
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDryAllow).Return(shared.MetricsSuccess)

		cfg := testConfig(t)
		cfg.dryRun = true
		cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":false}`)},
		)
	})

	t.Run("multi-chunk body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().ContinueRequest()
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionAllowed).Return(shared.MetricsSuccess)

		cb := &openfgaCallback{handle: mockHandle, config: testConfig(t), metrics: testMetrics()}
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{
				pkg.UnsafeBufferFromString(`{"allowed":tru`),
				pkg.UnsafeBufferFromString(`e}`),
			},
		)
	})

	t.Run("failure scenarios", func(t *testing.T) {
		tests := []struct {
			name           string
			result         shared.HttpCalloutResult
			headers        [][2]shared.UnsafeEnvoyBuffer
			body           []shared.UnsafeEnvoyBuffer
			failOpen       bool
			expectContinue bool
			expectStatus   uint32
			metric         string
		}{
			{
				name:           "callout failure fail_closed",
				result:         shared.HttpCalloutReset,
				headers:        nil,
				body:           nil,
				failOpen:       false,
				expectContinue: false,
				expectStatus:   http.StatusBadGateway,
				metric:         decisionError,
			},
			{
				name:           "callout failure fail_open",
				result:         shared.HttpCalloutReset,
				headers:        nil,
				body:           nil,
				failOpen:       true,
				expectContinue: true,
				metric:         decisionFailOpen,
			},
			{
				name:           "non-200 status fail_closed",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("500")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("error")},
				failOpen:       false,
				expectContinue: false,
				expectStatus:   http.StatusBadGateway,
				metric:         decisionError,
			},
			{
				name:           "non-200 status fail_open",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("500")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("error")},
				failOpen:       true,
				expectContinue: true,
				metric:         decisionFailOpen,
			},
			{
				name:           "invalid JSON fail_closed",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("{bad")},
				failOpen:       false,
				expectContinue: false,
				expectStatus:   http.StatusBadGateway,
				metric:         decisionError,
			},
			{
				name:           "invalid JSON fail_open",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("{bad")},
				failOpen:       true,
				expectContinue: true,
				metric:         decisionFailOpen,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()
				mockHandle := newMockFilterHandle(ctrl)

				cfg := testConfig(t)
				cfg.failOpen = tt.failOpen
				cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}

				if tt.expectContinue {
					mockHandle.EXPECT().ContinueRequest()
				} else {
					mockHandle.EXPECT().SendLocalResponse(tt.expectStatus, gomock.Any(), gomock.Any(), gomock.Any())
				}
				mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), tt.metric).Return(shared.MetricsSuccess)

				cb.OnHttpCalloutDone(0, tt.result, tt.headers, tt.body)
			})
		}
	})
}

func TestConfigFactory_Create(t *testing.T) {
	configJSON, err := json.Marshal(openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_access"},
		Object:      valueSource{Header: "x-resource", Prefix: "document:"},
	})
	require.NoError(t, err)

	factory := &OpenFGAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("openfga_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)
	mockHandle.EXPECT().DefineHistogram("openfga_check_duration_ms").Return(shared.MetricID(2), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_InvalidConfig(t *testing.T) {
	factory := &OpenFGAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	_, err := factory.Create(mockHandle, []byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration is required")

	_, err = factory.Create(mockHandle, []byte("{invalid"))
	require.Error(t, err)
}

func TestMetricsDisabled(t *testing.T) {
	metrics := &openfgaMetrics{enabled: false, hasCheckDur: false}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	metrics.inc(mockHandle, decisionAllowed)
	metrics.recordDuration(mockHandle, 100*time.Millisecond)
}

func TestRecordDuration_ConvertsToMilliseconds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	metrics := &openfgaMetrics{
		checkDuration: shared.MetricID(2),
		hasCheckDur:   true,
	}

	mockHandle.EXPECT().RecordHistogramValue(metrics.checkDuration, uint64(250))
	metrics.recordDuration(mockHandle, 250*time.Millisecond)
}

func TestRecordDuration_TruncatesMicroseconds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	metrics := &openfgaMetrics{
		checkDuration: shared.MetricID(2),
		hasCheckDur:   true,
	}

	mockHandle.EXPECT().RecordHistogramValue(metrics.checkDuration, uint64(1))
	metrics.recordDuration(mockHandle, 1500*time.Microsecond)
}

func TestFullFlow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	cfg := testConfig(t)
	f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id":  {"alice"},
		"x-resource": {"planning"},
	})

	mockHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Do(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) {
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":true}`)},
		)
	}).Return(shared.HttpCalloutInitSuccess, uint64(1))

	mockHandle.EXPECT().IncrementCounterValue(f.metrics.requestsTotal, uint64(1), decisionAllowed).Return(shared.MetricsSuccess)
	mockHandle.EXPECT().ContinueRequest()

	status := f.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestFullFlow_Denied(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	cfg := testConfig(t)
	f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id":  {"alice"},
		"x-resource": {"planning"},
	})

	mockHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Do(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) {
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":false}`)},
		)
	}).Return(shared.HttpCalloutInitSuccess, uint64(1))

	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), gomock.Any(), gomock.Any())
	mockHandle.EXPECT().IncrementCounterValue(f.metrics.requestsTotal, uint64(1), decisionDenied).Return(shared.MetricsSuccess)

	status := f.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
}

func TestCallback_EmptyBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), gomock.Any())
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionError).Return(shared.MetricsSuccess)

	cb := &openfgaCallback{handle: mockHandle, config: testConfig(t), metrics: testMetrics()}
	cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
		[]shared.UnsafeEnvoyBuffer{},
	)
}

func TestSendDeny(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	cfg := testConfig(t)
	mockHandle.EXPECT().SendLocalResponse(
		uint32(403), cfg.denyHeaders, cfg.denyBodyBytes, "openfga_denied",
	)

	sendDeny(mockHandle, cfg, "openfga_denied")
}

func TestCreatePerRoute(t *testing.T) {
	t.Run("valid config returns parsed config", func(t *testing.T) {
		configJSON, err := json.Marshal(openfgaConfig{
			Cluster:     "openfga",
			OpenFGAHost: "openfga:8080",
			StoreID:     "store1",
			User:        valueSource{Header: "x-user-id", Prefix: "user:"},
			Relation:    valueSource{Value: "can_access"},
			Object:      valueSource{Header: "x-resource", Prefix: "document:"},
		})
		require.NoError(t, err)

		factory := &OpenFGAHttpFilterConfigFactory{}
		result, err := factory.CreatePerRoute(configJSON)
		require.NoError(t, err)
		require.NotNil(t, result)

		cfg, ok := result.(*parsedConfig)
		require.True(t, ok)
		require.Equal(t, "openfga", cfg.cluster)
		require.Equal(t, "store1", cfg.storeID)
	})

	t.Run("empty config returns error", func(t *testing.T) {
		factory := &OpenFGAHttpFilterConfigFactory{}
		_, err := factory.CreatePerRoute(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "configuration is required")

		_, err = factory.CreatePerRoute([]byte{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "configuration is required")
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		factory := &OpenFGAHttpFilterConfigFactory{}
		_, err := factory.CreatePerRoute([]byte("{invalid"))
		require.Error(t, err)
	})

	t.Run("missing required fields returns error", func(t *testing.T) {
		factory := &OpenFGAHttpFilterConfigFactory{}
		_, err := factory.CreatePerRoute([]byte(`{"cluster":"openfga"}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing required field")
	})
}

func TestFilterFactory_PerRouteOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	globalCfg := testConfig(t)
	factory := &openfgaFilterFactory{config: globalCfg, metrics: testMetrics()}

	perRouteCfg := testConfig(t)
	perRouteCfg.storeID = "per-route-store"

	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().GetMostSpecificConfig().Return(perRouteCfg)

	filter := factory.Create(mockHandle)
	openfgaF, ok := filter.(*openfgaFilter)
	require.True(t, ok)
	require.Equal(t, "per-route-store", openfgaF.config.storeID)
}

func TestFilterFactory_NilConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	factory := &openfgaFilterFactory{config: nil, metrics: testMetrics()}

	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().GetMostSpecificConfig().Return(nil)

	filter := factory.Create(mockHandle)
	_, ok := filter.(*shared.EmptyHttpFilter)
	require.True(t, ok)
}

func TestDryRun_NoMatchingRule(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDryAllow).Return(shared.MetricsSuccess)

	cfg := testMultiRuleNoCatchAllConfig(t)
	cfg.dryRun = true
	f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id": {"alice"},
	})
	status := f.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestDryRun_MissingParameters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDryAllow).Return(shared.MetricsSuccess)

	cfg := testConfig(t)
	cfg.dryRun = true
	f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	// x-resource header is missing, so object resolves to empty
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id": {"alice"},
	})
	status := f.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestDryRun_CalloutInitFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDryAllow).Return(shared.MetricsSuccess)

	mockHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(shared.HttpCalloutInitClusterNotFound, uint64(0))

	cfg := testConfig(t)
	cfg.dryRun = true
	f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id":  {"alice"},
		"x-resource": {"planning"},
	})
	status := f.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestDryRun_CallbackError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().ContinueRequest()
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDryAllow).Return(shared.MetricsSuccess)

	cfg := testConfig(t)
	cfg.dryRun = true
	cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}
	cb.OnHttpCalloutDone(0, shared.HttpCalloutReset, nil, nil)
}

func TestOnRequestHeaders_ContextualTuples(t *testing.T) {
	t.Run("tuples included in check body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		cfg.contextualTuples = []parsedContextualTuple{
			{
				user:     valueSource{Header: "x-user-id", Prefix: "user:"},
				relation: valueSource{Value: "member", resolved: "member"},
				object:   valueSource{Header: "x-org-id", Prefix: "organization:"},
			},
		}
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
			"x-org-id":   {"acme"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		ctxTuples := body["contextual_tuples"].(map[string]any)
		tupleKeys := ctxTuples["tuple_keys"].([]any)
		require.Len(t, tupleKeys, 1)
		first := tupleKeys[0].(map[string]any)
		require.Equal(t, "user:alice", first["user"])
		require.Equal(t, "member", first["relation"])
		require.Equal(t, "organization:acme", first["object"])
	})

	t.Run("incomplete tuple is skipped", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		cfg.contextualTuples = []parsedContextualTuple{
			{
				user:     valueSource{Header: "x-user-id", Prefix: "user:"},
				relation: valueSource{Value: "member", resolved: "member"},
				object:   valueSource{Header: "x-org-id", Prefix: "organization:"}, // x-org-id not in request
			},
		}
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
			// x-org-id intentionally absent
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		_, hasCtxTuples := body["contextual_tuples"]
		require.False(t, hasCtxTuples, "incomplete contextual tuple should be skipped")
	})
}

func TestOnRequestHeaders_Context(t *testing.T) {
	t.Run("context values included in check body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		cfg.context = map[string]valueSource{
			"ip_address": {Header: "x-forwarded-for"},
			"region":     {Value: "us-east-1", resolved: "us-east-1"},
		}
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":       {"alice"},
			"x-resource":      {"planning"},
			"x-forwarded-for": {"10.0.0.1"},
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		ctx := body["context"].(map[string]any)
		require.Equal(t, "10.0.0.1", ctx["ip_address"])
		require.Equal(t, "us-east-1", ctx["region"])
	})

	t.Run("empty context values are omitted", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		cfg.context = map[string]valueSource{
			"ip_address": {Header: "x-forwarded-for"}, // header absent → empty
		}
		f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), gomock.Any()).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"x-user-id":  {"alice"},
			"x-resource": {"planning"},
			// x-forwarded-for intentionally absent
		})
		status := f.OnRequestHeaders(headers, false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		_, hasCtx := body["context"]
		require.False(t, hasCtx, "context with all empty values should be omitted")
	})
}

func TestCallback_CustomDenyStatusAndBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	cfg := testConfig(t)
	cfg.deny = pkg.LocalResponse{Status: 401, Body: "Unauthorized"}
	cfg.denyBodyBytes = []byte("Unauthorized")
	cfg.denyHeaders = [][2]string{{"content-type", "text/plain"}}

	mockHandle.EXPECT().SendLocalResponse(uint32(401), cfg.denyHeaders, cfg.denyBodyBytes, "openfga_denied")
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionDenied).Return(shared.MetricsSuccess)

	cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}
	cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
		[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":false}`)},
	)
}

func TestCallback_400StatusDiagnostic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	// Expect the specific 400 diagnostic log message
	mockHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), gomock.Any())
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionError).Return(shared.MetricsSuccess)

	cfg := testConfig(t)
	cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}
	cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("400")}},
		[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"code":"validation_error","message":"invalid tuple"}`)},
	)
}

func TestCallback_MetadataWritten(t *testing.T) {
	tests := []struct {
		name     string
		result   shared.HttpCalloutResult
		status   string
		body     string
		dryRun   bool
		failOpen bool
		decision string
	}{
		{
			name:     "allowed writes allowed metadata",
			result:   shared.HttpCalloutSuccess,
			status:   "200",
			body:     `{"allowed":true}`,
			decision: decisionAllowed,
		},
		{
			name:     "denied writes denied metadata",
			result:   shared.HttpCalloutSuccess,
			status:   "200",
			body:     `{"allowed":false}`,
			decision: decisionDenied,
		},
		{
			name:     "dry_run denied writes dryrun_allow metadata",
			result:   shared.HttpCalloutSuccess,
			status:   "200",
			body:     `{"allowed":false}`,
			dryRun:   true,
			decision: decisionDryAllow,
		},
		{
			name:     "error with fail_open writes failopen metadata",
			result:   shared.HttpCalloutReset,
			failOpen: true,
			decision: decisionFailOpen,
		},
		{
			name:     "error without fail_open writes error metadata",
			result:   shared.HttpCalloutReset,
			decision: decisionError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			mockHandle := newMockFilterHandle(ctrl)

			cfg := testConfig(t)
			cfg.metadata = &pkg.MetadataKey{Namespace: "openfga.authz", Key: "decision"}
			cfg.dryRun = tt.dryRun
			cfg.failOpen = tt.failOpen

			mockHandle.EXPECT().SetMetadata("openfga.authz", "decision", tt.decision)
			mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), tt.decision).Return(shared.MetricsSuccess)

			if tt.decision == decisionAllowed || tt.decision == decisionDryAllow || tt.decision == decisionFailOpen {
				mockHandle.EXPECT().ContinueRequest()
			} else {
				mockHandle.EXPECT().SendLocalResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			}

			cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}
			var headers [][2]shared.UnsafeEnvoyBuffer
			var body []shared.UnsafeEnvoyBuffer
			if tt.status != "" {
				headers = [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString(tt.status)}}
			}
			if tt.body != "" {
				body = []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(tt.body)}
			}
			cb.OnHttpCalloutDone(0, tt.result, headers, body)
		})
	}
}

func TestOnRequestHeaders_CustomCalloutHeaders(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	cfgJSON := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_access"},
		Object:      valueSource{Header: "x-resource", Prefix: "document:"},
		CalloutHeaders: map[string]string{
			"authorization": "Bearer my-token",
		},
	}
	raw, err := json.Marshal(cfgJSON)
	require.NoError(t, err)
	cfg, err := parseConfig(raw)
	require.NoError(t, err)

	f := &openfgaFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	var capturedHeaders [][2]string
	mockHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Do(func(_ string, headers [][2]string, _ []byte, _ uint64, _ shared.HttpCalloutCallback) {
		capturedHeaders = headers
	}).Return(shared.HttpCalloutInitSuccess, uint64(1))
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), gomock.Any()).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id":  {"alice"},
		"x-resource": {"planning"},
	})
	_ = f.OnRequestHeaders(headers, false)

	require.Equal(t, "Bearer my-token", headerValue(capturedHeaders, "authorization"))
}

func TestCallback_EmptyBody_FailOpen(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)
	mockHandle.EXPECT().ContinueRequest()
	mockHandle.EXPECT().IncrementCounterValue(gomock.Any(), uint64(1), decisionFailOpen).Return(shared.MetricsSuccess)

	cfg := testConfig(t)
	cfg.failOpen = true
	cb := &openfgaCallback{handle: mockHandle, config: cfg, metrics: testMetrics()}
	cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
		[]shared.UnsafeEnvoyBuffer{},
	)
}

func TestWriteMetadata(t *testing.T) {
	t.Run("with metadata configured", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		cfg.metadata = &pkg.MetadataKey{Namespace: "openfga.authz", Key: "decision"}

		mockHandle.EXPECT().SetMetadata("openfga.authz", "decision", decisionAllowed)

		writeMetadata(mockHandle, cfg, decisionAllowed)
	})

	t.Run("with nil metadata is no-op", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := testConfig(t)
		cfg.metadata = nil
		// No SetMetadata expectation — call must not panic or call SetMetadata.

		writeMetadata(mockHandle, cfg, decisionAllowed)
	})
}

func TestJoinCalloutBody(t *testing.T) {
	t.Run("empty body returns nil", func(t *testing.T) {
		require.Nil(t, joinCalloutBody(nil))
		require.Nil(t, joinCalloutBody([]shared.UnsafeEnvoyBuffer{}))
	})

	t.Run("single chunk returns that chunk", func(t *testing.T) {
		body := []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"allowed":true}`)}
		result := joinCalloutBody(body)
		require.Equal(t, `{"allowed":true}`, string(result))
	})

	t.Run("multiple chunks are joined", func(t *testing.T) {
		body := []shared.UnsafeEnvoyBuffer{
			pkg.UnsafeBufferFromString(`{"allowed":`),
			pkg.UnsafeBufferFromString(`true}`),
		}
		result := joinCalloutBody(body)
		require.Equal(t, `{"allowed":true}`, string(result))
	})

	t.Run("three chunks are joined", func(t *testing.T) {
		body := []shared.UnsafeEnvoyBuffer{
			pkg.UnsafeBufferFromString(`{"a`),
			pkg.UnsafeBufferFromString(`ll`),
			pkg.UnsafeBufferFromString(`owed":true}`),
		}
		result := joinCalloutBody(body)
		require.Equal(t, `{"allowed":true}`, string(result))
	})
}

func TestCalloutHeaderValue(t *testing.T) {
	headers := [][2]shared.UnsafeEnvoyBuffer{
		{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
		{pkg.UnsafeBufferFromString("content-type"), pkg.UnsafeBufferFromString("application/json")},
	}

	t.Run("existing key returns value", func(t *testing.T) {
		require.Equal(t, "200", calloutHeaderValue(headers, ":status"))
		require.Equal(t, "application/json", calloutHeaderValue(headers, "content-type"))
	})

	t.Run("missing key returns empty string", func(t *testing.T) {
		require.Empty(t, calloutHeaderValue(headers, "x-missing"))
	})

	t.Run("nil headers returns empty string", func(t *testing.T) {
		require.Empty(t, calloutHeaderValue(nil, ":status"))
	})
}
