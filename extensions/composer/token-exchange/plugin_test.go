// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oauth2te

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// mustParseConfig marshals cfg to JSON and runs it through parseConfig so that
// precomputed fields (e.g. calloutHeaders) are populated.
func mustParseConfig(t *testing.T, cfg *tokenExchangeConfig) *tokenExchangeConfig {
	t.Helper()
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	parsed, err := parseConfig(b)
	require.NoError(t, err)
	return parsed
}

// testMetrics returns an tokenExchangeMetrics with all metrics enabled for testing.
func testMetrics() *tokenExchangeMetrics {
	return &tokenExchangeMetrics{
		exchanges:          shared.MetricID(1),
		hasExchanges:       true,
		exchangeResults:    shared.MetricID(2),
		hasExchangeResults: true,
	}
}

func newMockFilterHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return mockHandle
}

func TestOnRequestHeaders(t *testing.T) {
	t.Run("auth errors", func(t *testing.T) {
		tests := []struct {
			name    string
			headers map[string][]string
		}{
			{"missing authorization", map[string][]string{}},
			{"non-bearer auth", map[string][]string{"authorization": {"Basic xxx"}}},
			{"empty bearer token", map[string][]string{"authorization": {"Bearer "}}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()
				mockHandle := newMockFilterHandle(ctrl)
				mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusUnauthorized), gomock.Any(), gomock.Any(), gomock.Any())

				f := &tokenExchangeFilter{handle: mockHandle, config: &tokenExchangeConfig{}, metrics: testMetrics()}
				status := f.OnRequestHeaders(fake.NewFakeHeaderMap(tt.headers), false)
				require.Equal(t, shared.HeadersStatusStop, status)
			})
		}
	})

	t.Run("valid token callout succeeds", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := mustParseConfig(t, &tokenExchangeConfig{
			Cluster:          "sts_cluster",
			TokenExchangeURL: "sts.example.com/oauth2/token",
			ClientID:         "my-client",
			ClientSecret:     "my-secret",
		})

		f := &tokenExchangeFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var (
			capturedCluster string
			capturedHeaders [][2]shared.UnsafeEnvoyBuffer
			capturedBody    []byte
			capturedTimeout uint64
		)

		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(cluster string, headers [][2]string, body []byte, timeout uint64, _ shared.HttpCalloutCallback) {
			capturedCluster = cluster
			for _, h := range headers {
				capturedHeaders = append(capturedHeaders, [2]shared.UnsafeEnvoyBuffer{
					pkg.UnsafeBufferFromString(h[0]),
					pkg.UnsafeBufferFromString(h[1]),
				})
			}
			capturedBody = body
			capturedTimeout = timeout
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(f.metrics.exchanges, uint64(1)).Return(shared.MetricsSuccess)
		status := f.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{
			"authorization": {"Bearer mytoken"},
		}), false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		// Verify callout parameters.
		require.Equal(t, "sts_cluster", capturedCluster)
		require.Equal(t, uint64(5000), capturedTimeout)

		// Verify headers.
		require.Equal(t, "POST", headerValue(capturedHeaders, ":method"))
		require.Equal(t, "/oauth2/token", headerValue(capturedHeaders, ":path"))
		require.Equal(t, "sts.example.com", headerValue(capturedHeaders, "host"))
		require.Equal(t, "application/x-www-form-urlencoded", headerValue(capturedHeaders, "content-type"))

		expectedCreds := base64.StdEncoding.EncodeToString([]byte("my-client:my-secret"))
		require.Equal(t, "Basic "+expectedCreds, headerValue(capturedHeaders, "authorization"))

		// Verify body form values.
		form, err := url.ParseQuery(string(capturedBody))
		require.NoError(t, err)
		require.Equal(t, grantTypeTokenExchange, form.Get("grant_type"))
		require.Equal(t, "mytoken", form.Get("subject_token"))
		require.Equal(t, defaultSubjectTokenType, form.Get("subject_token_type"))
		require.Empty(t, form.Get("audience"))
		require.Empty(t, form.Get("resource"))
		require.Empty(t, form.Get("scope"))
		require.Empty(t, form.Get("actor_token"))
	})

	t.Run("valid token with optional fields", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)

		cfg := mustParseConfig(t, &tokenExchangeConfig{
			Cluster:            "sts_cluster",
			TokenExchangeURL:   "sts.example.com/oauth2/token",
			ClientID:           "my-client",
			ClientSecret:       "my-secret",
			Audience:           "https://api.example.com",
			Resource:           "https://api.example.com/v1",
			Scope:              "read write",
			RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
			ActorToken:         "actor-tok",
			ActorTokenType:     "urn:ietf:params:oauth:token-type:access_token",
		})

		f := &tokenExchangeFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

		var capturedBody []byte
		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Do(func(_ string, _ [][2]string, body []byte, _ uint64, _ shared.HttpCalloutCallback) {
			capturedBody = body
		}).Return(shared.HttpCalloutInitSuccess, uint64(1))
		mockHandle.EXPECT().IncrementCounterValue(f.metrics.exchanges, uint64(1)).Return(shared.MetricsSuccess)
		status := f.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{
			"authorization": {"Bearer mytoken"},
		}), false)
		require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)

		form, err := url.ParseQuery(string(capturedBody))
		require.NoError(t, err)
		require.Equal(t, "https://api.example.com", form.Get("audience"))
		require.Equal(t, "https://api.example.com/v1", form.Get("resource"))
		require.Equal(t, "read write", form.Get("scope"))
		require.Equal(t, "urn:ietf:params:oauth:token-type:access_token", form.Get("requested_token_type"))
		require.Equal(t, "actor-tok", form.Get("actor_token"))
		require.Equal(t, "urn:ietf:params:oauth:token-type:access_token", form.Get("actor_token_type"))
	})

	t.Run("callout init failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), gomock.Any())

		mockHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(shared.HttpCalloutInitClusterNotFound, uint64(0))

		cfg := mustParseConfig(t, &tokenExchangeConfig{
			Cluster:          "bad_cluster",
			TokenExchangeURL: "h/t",
			ClientID:         "c",
			ClientSecret:     "s",
		})

		f := &tokenExchangeFilter{handle: mockHandle, config: cfg}
		status := f.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{
			"authorization": {"Bearer tok"},
		}), false)
		require.Equal(t, shared.HeadersStatusStop, status)
	})
}

func TestSTSCallback(t *testing.T) {
	t.Run("successful exchange", func(t *testing.T) {
		tests := []struct {
			name string
			body []shared.UnsafeEnvoyBuffer
		}{
			{
				"single chunk",
				[]shared.UnsafeEnvoyBuffer{
					pkg.UnsafeBufferFromString(`{"access_token":"new-token","token_type":"Bearer","issued_token_type":"urn:ietf:params:oauth:token-type:access_token"}`),
				},
			},
			{
				"multi-chunk",
				[]shared.UnsafeEnvoyBuffer{
					pkg.UnsafeBufferFromString(`{"access_token":"new-tok`),
					pkg.UnsafeBufferFromString(`en","token_type":"Bearer","issued_token_type":"urn:ietf:params:oauth:token-type:access_token"}`),
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()
				mockHandle := newMockFilterHandle(ctrl)
				cb := &tokenExchangeCallback{handle: mockHandle, metrics: testMetrics()}

				reqHeaders := fake.NewFakeHeaderMap(map[string][]string{
					"authorization": {"Bearer old-token"},
				})
				mockHandle.EXPECT().RequestHeaders().Return(reqHeaders)
				mockHandle.EXPECT().ContinueRequest()
				mockHandle.EXPECT().IncrementCounterValue(cb.metrics.exchangeResults, uint64(1), "success").Return(shared.MetricsSuccess)
				cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
					[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
					tt.body,
				)
				require.Equal(t, "Bearer new-token", reqHeaders.GetOne("authorization").ToUnsafeString())
			})
		}
	})

	t.Run("failed exchange", func(t *testing.T) {
		tests := []struct {
			name           string
			result         shared.HttpCalloutResult
			headers        [][2]shared.UnsafeEnvoyBuffer
			body           []shared.UnsafeEnvoyBuffer
			expectedStatus uint32
			metricResult   string
		}{
			{
				name:           "callout failure",
				result:         shared.HttpCalloutReset,
				headers:        nil,
				body:           nil,
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "missing status header",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{},
				body:           nil,
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "4xx with RFC error body",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("403")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"error":"access_denied","error_description":"error description"}`)},
				expectedStatus: http.StatusUnauthorized,
				metricResult:   metricResRejected,
			},
			{
				name:           "4xx with non-JSON body",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("400")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("bad request")},
				expectedStatus: http.StatusUnauthorized,
				metricResult:   metricResRejected,
			},
			{
				name:           "5xx status",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("500")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("internal error")},
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "invalid JSON body",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("{bad")},
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "missing access_token",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"token_type":"Bearer","issued_token_type":"urn:ietf:params:oauth:token-type:access_token"}`)},
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "missing token_type",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"access_token":"tok","issued_token_type":"urn:ietf:params:oauth:token-type:access_token"}`)},
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "missing issued_token_type",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"access_token":"tok","token_type":"Bearer"}`)},
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
			{
				name:           "token_type N_A",
				result:         shared.HttpCalloutSuccess,
				headers:        [][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
				body:           []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"access_token":"tok","token_type":"N_A","issued_token_type":"urn:ietf:params:oauth:token-type:access_token"}`)},
				expectedStatus: http.StatusBadGateway,
				metricResult:   metricResError,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()
				mockHandle := newMockFilterHandle(ctrl)
				cb := &tokenExchangeCallback{handle: mockHandle, metrics: testMetrics()}

				mockHandle.EXPECT().SendLocalResponse(tt.expectedStatus, gomock.Any(), gomock.Any(), gomock.Any())
				mockHandle.EXPECT().IncrementCounterValue(cb.metrics.exchangeResults, uint64(1), tt.metricResult).Return(shared.MetricsSuccess)
				cb.OnHttpCalloutDone(0, tt.result, tt.headers, tt.body)
			})
		}
	})
}

func TestFullExchange(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newMockFilterHandle(ctrl)

	cfg := mustParseConfig(t, &tokenExchangeConfig{
		Cluster:          "cluster",
		TokenExchangeURL: "host/path",
		ClientID:         "client",
		ClientSecret:     "client-secret",
	})

	reqHeaders := fake.NewFakeHeaderMap(map[string][]string{
		"authorization": {"Bearer original"},
	})

	f := &tokenExchangeFilter{handle: mockHandle, config: cfg, metrics: testMetrics()}

	// Capture the callback and invoke it inline to simulate the STS response.
	mockHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Do(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) {
		cb.OnHttpCalloutDone(0, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")}},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(`{"access_token":"new-token","token_type":"Bearer","issued_token_type":"urn:ietf:params:oauth:token-type:access_token"}`)},
		)
	}).Return(shared.HttpCalloutInitSuccess, uint64(1))

	mockHandle.EXPECT().IncrementCounterValue(f.metrics.exchanges, uint64(1)).Return(shared.MetricsSuccess)
	mockHandle.EXPECT().IncrementCounterValue(f.metrics.exchangeResults, uint64(1), "success").Return(shared.MetricsSuccess)
	mockHandle.EXPECT().RequestHeaders().Return(reqHeaders)
	mockHandle.EXPECT().ContinueRequest()

	status := f.OnRequestHeaders(reqHeaders, false)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Equal(t, "Bearer new-token", reqHeaders.GetOne("authorization").ToUnsafeString())
}

func newFilterHandleWithoutPerRouteConfig(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func newFilterHandleWithPerRouteConfig(ctrl *gomock.Controller, perRouteConfig any) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(perRouteConfig).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func Test_CreatePerRoute(t *testing.T) {
	f := &tokenExchangeHttpFilterConfigFactory{}

	t.Run("valid config", func(t *testing.T) {
		cfg := &tokenExchangeConfig{
			Cluster:          "sts_cluster",
			TokenExchangeURL: "sts.example.com/token",
			ClientID:         "client",
			ClientSecret:     "secret",
		}
		b, _ := json.Marshal(cfg)
		result, err := f.CreatePerRoute(b)
		require.NoError(t, err)
		require.NotNil(t, result)
		perRoute, ok := result.(*tokenExchangeConfig)
		require.True(t, ok)
		require.Equal(t, "sts_cluster", perRoute.Cluster)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		result, err := f.CreatePerRoute([]byte(`{invalid`))
		require.Error(t, err)
		require.Nil(t, result)
	})
}

func Test_PerRouteConfigOverride(t *testing.T) {
	baseConfig := mustParseConfig(t, &tokenExchangeConfig{
		Cluster:          "base_cluster",
		TokenExchangeURL: "base.example.com/token",
		ClientID:         "base_client",
		ClientSecret:     "base_secret",
	})
	baseMetrics := testMetrics()
	baseFactory := &tokenExchangeFilterFactory{config: baseConfig, metrics: baseMetrics}

	t.Run("per-route config overrides factory config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		perRouteConfig := mustParseConfig(t, &tokenExchangeConfig{
			Cluster:          "route_cluster",
			TokenExchangeURL: "route.example.com/token",
			ClientID:         "route_client",
			ClientSecret:     "route_secret",
		})
		perRoute := perRouteConfig
		handle := newFilterHandleWithPerRouteConfig(ctrl, perRoute)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*tokenExchangeFilter)
		require.True(t, ok)
		require.Equal(t, "route_cluster", f.config.Cluster)
		require.Equal(t, baseMetrics, f.metrics)
	})

	t.Run("nil per-route config uses factory config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		handle := newFilterHandleWithoutPerRouteConfig(ctrl)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*tokenExchangeFilter)
		require.True(t, ok)
		require.Equal(t, "base_cluster", f.config.Cluster)
	})

	t.Run("wrong type per-route config uses factory config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		handle := newFilterHandleWithPerRouteConfig(ctrl, "not-a-per-route-config")
		filter := baseFactory.Create(handle)
		f, ok := filter.(*tokenExchangeFilter)
		require.True(t, ok)
		require.Equal(t, "base_cluster", f.config.Cluster)
	})
}
