// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openfga

import (
	"encoding/json"
	"net/http"
	"testing"
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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

// testHeaders converts [][2]string to [][2]shared.UnsafeEnvoyBuffer for tests.
func testHeaders(h [][2]string) [][2]shared.UnsafeEnvoyBuffer {
	result := make([][2]shared.UnsafeEnvoyBuffer, len(h))
	for i, p := range h {
		result[i] = [2]shared.UnsafeEnvoyBuffer{
			{Ptr: unsafe.StringData(p[0]), Len: uint64(len(p[0]))},
			{Ptr: unsafe.StringData(p[1]), Len: uint64(len(p[1]))},
		}
	}
	return result
}

// testBody converts [][]byte to []shared.UnsafeEnvoyBuffer for tests.
func testBody(chunks [][]byte) []shared.UnsafeEnvoyBuffer {
	if chunks == nil {
		return nil
	}
	result := make([]shared.UnsafeEnvoyBuffer, len(chunks))
	for i, c := range chunks {
		if len(c) > 0 {
			result[i] = shared.UnsafeEnvoyBuffer{Ptr: unsafe.SliceData(c), Len: uint64(len(c))}
		}
	}
	return result
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
		require.Equal(t, "POST", headerValue(testHeaders(capturedHeaders), ":method"))
		require.Equal(t, "/stores/store1/check", headerValue(testHeaders(capturedHeaders), ":path"))
		require.Equal(t, "openfga:8080", headerValue(testHeaders(capturedHeaders), "host"))
		require.Equal(t, "application/json", headerValue(testHeaders(capturedHeaders), "content-type"))

		var body map[string]any
		require.NoError(t, json.Unmarshal(capturedBody, &body))
		tk := body["tuple_key"].(map[string]any)
		require.Equal(t, "user:alice", tk["user"])
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
			testHeaders([][2]string{{":status", "200"}}),
			testBody([][]byte{[]byte(`{"allowed":true}`)}),
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
			testHeaders([][2]string{{":status", "200"}}),
			testBody([][]byte{[]byte(`{"allowed":false}`)}),
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
			testHeaders([][2]string{{":status", "200"}}),
			testBody([][]byte{[]byte(`{"allowed":false}`)}),
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
			testHeaders([][2]string{{":status", "200"}}),
			testBody([][]byte{
				[]byte(`{"allowed":tru`),
				[]byte(`e}`),
			}),
		)
	})

	t.Run("failure scenarios", func(t *testing.T) {
		tests := []struct {
			name           string
			result         shared.HttpCalloutResult
			headers        [][2]string
			body           [][]byte
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
				headers:        [][2]string{{":status", "500"}},
				body:           [][]byte{[]byte("error")},
				failOpen:       false,
				expectContinue: false,
				expectStatus:   http.StatusBadGateway,
				metric:         decisionError,
			},
			{
				name:           "non-200 status fail_open",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]string{{":status", "500"}},
				body:           [][]byte{[]byte("error")},
				failOpen:       true,
				expectContinue: true,
				metric:         decisionFailOpen,
			},
			{
				name:           "invalid JSON fail_closed",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]string{{":status", "200"}},
				body:           [][]byte{[]byte("{bad")},
				failOpen:       false,
				expectContinue: false,
				expectStatus:   http.StatusBadGateway,
				metric:         decisionError,
			},
			{
				name:           "invalid JSON fail_open",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]string{{":status", "200"}},
				body:           [][]byte{[]byte("{bad")},
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

				cb.OnHttpCalloutDone(0, tt.result, testHeaders(tt.headers), testBody(tt.body))
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
	metrics.recordDuration(mockHandle, 100)
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
			testHeaders([][2]string{{":status", "200"}}),
			testBody([][]byte{[]byte(`{"allowed":true}`)}),
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
			testHeaders([][2]string{{":status", "200"}}),
			testBody([][]byte{[]byte(`{"allowed":false}`)}),
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
		testHeaders([][2]string{{":status", "200"}}),
		testBody([][]byte{}),
	)
}
