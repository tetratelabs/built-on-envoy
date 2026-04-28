// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"encoding/json"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// defaultCfg returns a config with one OpenAI rule and the default namespace.
// LLMFactory is set so that OnRequestHeaders can proceed past the nil-factory guard.
func defaultCfg() *llmProxyConfig {
	return &llmProxyConfig{
		LLMConfigs: []llmConfig{{
			Matcher: prefixMatcher("/v1/chat/completions"),
			Kind:    KindOpenAI,
			Factory: &openaiFactory{},
		}},
		MetadataNamespace: defaultMetadataNamespace,
	}
}

// prefixMatcher returns a StringMatcher that Matches by path prefix.
func prefixMatcher(prefix string) pkg.StringMatcher {
	return pkg.StringMatcher{Prefix: prefix}
}

// expectStatsDefinitions registers AnyTimes expectations for the 5 counter and 2 histogram
// metric definitions that llmProxyConfigFactory.Create always makes via newLLMProxyStats.
func expectStatsDefinitions(h *mocks.MockHttpFilterConfigHandle) {
	id := shared.MetricID(0)
	nextID := func(_ string, _ ...string) (shared.MetricID, shared.MetricsResult) {
		id++
		return id, shared.MetricsSuccess
	}
	h.EXPECT().DefineCounter(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(5)
	h.EXPECT().DefineHistogram(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(2)
}

// newPluginHandleWithoutPerRouteConfig creates a mock HttpFilterHandle with default expectations including
// GetMostSpecificConfig returning nil (no per-route config).
func newPluginHandleWithoutPerRouteConfig(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	pluginHandle.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	return pluginHandle
}

// newPluginHandleWithPerRouteConfig creates a mock HttpFilterHandle that returns
// the given per-route config from GetMostSpecificConfig.
func newPluginHandleWithPerRouteConfig(ctrl *gomock.Controller, perRouteConfig any) *mocks.MockHttpFilterHandle {
	pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	pluginHandle.EXPECT().GetMostSpecificConfig().Return(perRouteConfig).AnyTimes()
	return pluginHandle
}

// --- llmProxyConfigFactory.Create ---

func TestConfigFactory_Create_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockCfgHandle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	expectStatsDefinitions(mockCfgHandle)

	f := &llmProxyConfigFactory{}
	ff, err := f.Create(mockCfgHandle, []byte{})
	require.NoError(t, err)
	require.NotNil(t, ff)

	lff, ok := ff.(*llmProxyFilterFactory)
	require.True(t, ok)
	require.Equal(t, defaultMetadataNamespace, lff.config.MetadataNamespace)

	// The default config has support for both OpenAI and Anthropic.
	require.Len(t, lff.config.LLMConfigs, 2)
	require.Equal(t, KindAnthropic, lff.config.LLMConfigs[0].Kind)
	require.Equal(t, KindOpenAI, lff.config.LLMConfigs[1].Kind)
}

func TestConfigFactory_Create_CustomNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockCfgHandle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	expectStatsDefinitions(mockCfgHandle)

	f := &llmProxyConfigFactory{}
	// Provide one explicit OpenAI rule; ValidateAndParse will prepend a default Anthropic rule.
	cfg := `{"metadata_namespace":"custom-ns","llm_configs":[{"matcher":{"prefix":"/v1/chat/completions"},"kind":"openai"}]}`
	ff, err := f.Create(mockCfgHandle, []byte(cfg))
	require.NoError(t, err)

	lff, ok := ff.(*llmProxyFilterFactory)
	require.True(t, ok)
	require.Equal(t, "custom-ns", lff.config.MetadataNamespace)
	// The explicit OpenAI rule is kept; a default Anthropic rule is prepended.
	require.Len(t, lff.config.LLMConfigs, 2)
	require.Equal(t, KindAnthropic, lff.config.LLMConfigs[0].Kind)
	require.Equal(t, KindOpenAI, lff.config.LLMConfigs[1].Kind)
}

func TestConfigFactory_Create_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockCfgHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any()).Times(1)

	f := &llmProxyConfigFactory{}
	_, err := f.Create(mockCfgHandle, []byte(`{bad}`))
	require.Error(t, err)
}

// --- llmProxyFilterFactory.Create ---

func TestFilterFactory_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := newPluginHandleWithoutPerRouteConfig(ctrl)

	cfg := defaultCfg()
	ff := &llmProxyFilterFactory{config: cfg}
	filter := ff.Create(mockHandle)

	require.NotNil(t, filter)
	lf, ok := filter.(*llmProxyFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, lf.handle)
	require.Equal(t, cfg, lf.config)
}

func Test_CreatePerRoute(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := map[string]any{
			"metadata_namespace": "io.test",
			"llm_configs": []map[string]any{
				{"matcher": map[string]any{"suffix": "/v1/chat/completions"}, "kind": "openai"},
			},
		}
		configJSON, _ := json.Marshal(config)
		result, err := (&llmProxyConfigFactory{}).CreatePerRoute(configJSON)
		require.NoError(t, err)
		require.NotNil(t, result)
		perRoute, ok := result.(*llmProxyConfig)
		require.True(t, ok)
		assert.Equal(t, "io.test", perRoute.MetadataNamespace)
	})

	t.Run("empty config uses defaults", func(t *testing.T) {
		result, err := (&llmProxyConfigFactory{}).CreatePerRoute([]byte{})
		require.NoError(t, err)
		perRoute, ok := result.(*llmProxyConfig)
		require.True(t, ok)
		assert.Equal(t, defaultMetadataNamespace, perRoute.MetadataNamespace)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		result, err := (&llmProxyConfigFactory{}).CreatePerRoute([]byte(`{invalid`))
		require.Error(t, err)
		require.Nil(t, result)
	})
}

func Test_PerRouteConfigOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseFactory := &llmProxyFilterFactory{config: defaultCfg()}

	t.Run("per-route config overrides factory config", func(t *testing.T) {
		perRouteConfig := &llmProxyConfig{
			LLMConfigs:        []llmConfig{},
			MetadataNamespace: "io.per.route",
		}
		mockHandle := newPluginHandleWithPerRouteConfig(ctrl, perRouteConfig)
		filter := baseFactory.Create(mockHandle)
		lf, ok := filter.(*llmProxyFilter)
		require.True(t, ok)
		assert.Equal(t, "io.per.route", lf.config.MetadataNamespace)
	})

	t.Run("nil per-route config uses factory config", func(t *testing.T) {
		mockHandle := newPluginHandleWithoutPerRouteConfig(ctrl)
		filter := baseFactory.Create(mockHandle)
		lf, ok := filter.(*llmProxyFilter)
		require.True(t, ok)
		assert.Equal(t, defaultMetadataNamespace, lf.config.MetadataNamespace)
	})

	t.Run("non-matching per-route config type uses factory config", func(t *testing.T) {
		mockHandle := newPluginHandleWithPerRouteConfig(ctrl, "not-a-per-route-config")
		filter := baseFactory.Create(mockHandle)
		lf, ok := filter.(*llmProxyFilter)
		require.True(t, ok)
		assert.Equal(t, defaultMetadataNamespace, lf.config.MetadataNamespace)
	})
}

// --- WellKnownHttpFilterConfigFactories ---

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, ExtensionName)
	_, ok := factories[ExtensionName].(*llmProxyConfigFactory)
	require.True(t, ok)
}

// --- matchRule ---

func TestMatchRule_FirstMatch(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{
			{Matcher: prefixMatcher("/v1/chat/completions"), Kind: KindOpenAI},
			{Matcher: prefixMatcher("/v1/messages"), Kind: KindAnthropic},
		},
	}
	f := &llmProxyFilter{config: cfg}
	rule := f.matchRule("/v1/messages")
	require.NotNil(t, rule)
	require.Equal(t, KindAnthropic, rule.Kind)
}

func TestMatchRule_PrefixMatch(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{{Matcher: prefixMatcher("/v1/chat"), Kind: KindOpenAI}},
	}
	f := &llmProxyFilter{config: cfg}
	rule := f.matchRule("/v1/chat/completions?model=gpt-4o")
	require.NotNil(t, rule)
	require.Equal(t, KindOpenAI, rule.Kind)
}

func TestMatchRule_NoMatch(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{{Matcher: prefixMatcher("/v1/chat/completions"), Kind: KindOpenAI}},
	}
	f := &llmProxyFilter{config: cfg}
	require.Nil(t, f.matchRule("/other"))
}

func TestMatchRule_EmptyRules(t *testing.T) {
	f := &llmProxyFilter{config: &llmProxyConfig{}}
	require.Nil(t, f.matchRule("/v1/chat/completions"))
}

// --- OnRequestHeaders ---

func TestOnRequestHeaders_UnknownPath_Passthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/unknown"), true)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg()}
	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	result := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.False(t, filter.matched)
}

func TestOnRequestHeaders_MatchedPath_EndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any(), gomock.Any()).Times(1) // matched path
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)               // error
	mockHandle.EXPECT().IncrementCounterValue(idRequestError, uint64(1), "openai", "").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), stats: newTestStats(ctrl)}
	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	result := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.True(t, filter.matched)  // path was matched before the endOfStream check
	require.True(t, filter.hasError) // endOfStream on a matched path is an error
}

func TestOnRequestHeaders_MatchedPath_HasBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any(), gomock.Any()).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg()}
	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	result := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, result)
	require.True(t, filter.matched)
}

func TestOnRequestHeaders_NonJSONContentType_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any(), gomock.Any()).Times(1) // matched path
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)               // error
	mockHandle.EXPECT().IncrementCounterValue(idRequestError, uint64(1), "openai", "").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), stats: newTestStats(ctrl)}
	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/plain"}})
	result := filter.OnRequestHeaders(headers, false)
	// Request is passed through (Continue) but processing is skipped due to the error.
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.True(t, filter.matched)  // path matched before the content-type check
	require.True(t, filter.hasError) // non-JSON content-type triggers an error
}

// --- OnRequestBody ---

func TestOnRequestBody_NotMatched_Passthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: false}
	result := filter.OnRequestBody(fake.NewFakeBodyBuffer([]byte(`{}`)), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: true, factory: &openaiFactory{}}
	result := filter.OnRequestBody(fake.NewFakeBodyBuffer([]byte(`partial`)), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnRequestBody_OpenAI_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"model":"gpt-4o","stream":false}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "kind", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "is_stream", false).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  defaultCfg(),
		stats:   newTestStats(ctrl),
		matched: true,
		kind:    KindOpenAI,
		factory: &openaiFactory{},
	}
	result := filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
	require.NotNil(t, filter.llmReq)
}

func TestOnRequestBody_Anthropic_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	cfg := &llmProxyConfig{
		LLMConfigs:        []llmConfig{{Matcher: prefixMatcher("/v1/messages"), Kind: KindAnthropic}},
		MetadataNamespace: defaultMetadataNamespace,
	}
	body := []byte(`{"model":"claude-3-5-sonnet-20241022","stream":true}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "kind", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "model", "claude-3-5-sonnet-20241022").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "is_stream", true).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "anthropic", "claude-3-5-sonnet-20241022").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  cfg,
		stats:   newTestStats(ctrl),
		matched: true,
		kind:    KindAnthropic,
		factory: &anthropicFactory{},
	}
	result := filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_InvalidJSON_LogsDebug(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{bad json}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestError, uint64(1), "", "").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "", "").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), stats: newTestStats(ctrl), matched: true, factory: &openaiFactory{}}
	result := filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
	// Graceful degradation: continue even on parse failure.
	require.Equal(t, shared.BodyStatusContinue, result)
}

// --- OnRequestTrailers ---

func TestOnRequestTrailers_NotProcessed_ParsesBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"model":"gpt-4o","stream":false}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "kind", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "is_stream", false).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  defaultCfg(),
		stats:   newTestStats(ctrl),
		matched: true,
		kind:    KindOpenAI,
		factory: &openaiFactory{},
	}
	result := filter.OnRequestTrailers(fake.NewFakeHeaderMap(nil))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

func TestOnRequestTrailers_HasError_Noop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: true, hasError: true}
	result := filter.OnRequestTrailers(fake.NewFakeHeaderMap(nil))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

// --- OnResponseHeaders ---

func TestOnResponseHeaders_NotMatched_Continue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: false}
	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	result := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, result)
}

func TestOnResponseHeaders_EndOfStream_Continue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestError, uint64(1), "", "").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "", "").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), stats: newTestStats(ctrl), matched: true}
	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	result := filter.OnResponseHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.True(t, filter.hasError)
}

func TestOnResponseHeaders_SSE_SetsParser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	// "llm-proxy: handling SSE response" has no format args → 0 variadic args.
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any()).Times(1)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: true, factory: &openaiFactory{}}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}, "content-type": {"text/event-stream"}})
	result := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.NotNil(t, filter.sseParser)
}

func TestOnResponseHeaders_JSON_Stop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: true, factory: &openaiFactory{}}
	headers := fake.NewFakeHeaderMap(map[string][]string{":status": {"200"}, "content-type": {"application/json"}})
	result := filter.OnResponseHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, result)
	require.Nil(t, filter.sseParser)
}

// --- OnResponseBody (non-streaming) ---

func TestOnResponseBody_NotMatched_Passthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: false}
	result := filter.OnResponseBody(fake.NewFakeBodyBuffer([]byte(`{}`)), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_NonStreaming_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: true, factory: &openaiFactory{}}
	result := filter.OnResponseBody(fake.NewFakeBodyBuffer([]byte(`partial`)), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnResponseBody_OpenAI_SetsUsageMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedResponseBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(10)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(20)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(30)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	// Set some field values that should be set in the request phase.
	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  defaultCfg(),
		stats:   newTestStats(ctrl),
		matched: true,
		factory: &openaiFactory{},
		model:   "gpt-4o",
		kind:    KindOpenAI,
	}
	result := filter.OnResponseBody(fake.NewFakeBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
	require.NotNil(t, filter.llmResp)
}

func TestOnResponseBody_Anthropic_SetsUsageMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	cfg := &llmProxyConfig{
		LLMConfigs:        []llmConfig{{Matcher: prefixMatcher("/v1/messages"), Kind: KindAnthropic}},
		MetadataNamespace: defaultMetadataNamespace,
	}
	body := []byte(`{"id":"m1","type":"message","usage":{"input_tokens":15,"output_tokens":5}}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedResponseBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(15)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(5)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(20)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(15), "anthropic", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(20), "anthropic", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	// Set some field values that should be set in the request phase.
	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  cfg,
		stats:   newTestStats(ctrl),
		matched: true,
		factory: &anthropicFactory{},
		model:   "gpt-4o",
		kind:    KindAnthropic,
	}
	result := filter.OnResponseBody(fake.NewFakeBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

// TestOnResponseBody_NoUsageInResponse verifies that onResponseSuccess always sets
// all three token metadata keys (even as zero values) when no usage is present.
func TestOnResponseBody_NoUsageInResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"choices":[]}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedResponseBody().Return(true).Times(1)
	// onResponseSuccess always sets all three keys, even when they are zero.
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(0)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(0)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(0)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(0), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(0), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(0), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  defaultCfg(),
		stats:   newTestStats(ctrl),
		matched: true,
		factory: &openaiFactory{},
		model:   "gpt-4o",
		kind:    KindOpenAI,
	}
	result := filter.OnResponseBody(fake.NewFakeBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

// --- OnResponseBody (streaming SSE) ---

func TestOnResponseBody_SSE_OpenAI_AccumulatesAndSetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(8)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(4)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(12)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(8), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(4), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(12), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	acc := newOpenAISSEParser()
	filter := &llmProxyFilter{
		handle:    mockHandle,
		config:    defaultCfg(),
		stats:     newTestStats(ctrl),
		matched:   true,
		factory:   &openaiFactory{},
		sseParser: acc,
		model:     "gpt-4o",
		kind:      KindOpenAI,
	}

	chunk1 := fake.NewFakeBodyBuffer([]byte("data: {\"choices\":[]}\n"))
	chunk2 := fake.NewFakeBodyBuffer([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n"))
	done := fake.NewFakeBodyBuffer([]byte("data: [DONE]\n"))

	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(chunk1, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(chunk2, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(done, true))
	require.NotNil(t, filter.llmResp)
}

func TestOnResponseBody_SSE_Anthropic_AccumulatesAndSetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(25)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(10)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(35)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(25), "anthropic", "claude-1").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(10), "anthropic", "claude-1").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(35), "anthropic", "claude-1").Return(shared.MetricsSuccess).Times(1)

	acc := newAnthropicSSEParser()
	cfg := &llmProxyConfig{
		LLMConfigs:        []llmConfig{{Matcher: prefixMatcher("/v1/messages"), Kind: KindAnthropic}},
		MetadataNamespace: defaultMetadataNamespace,
	}
	filter := &llmProxyFilter{
		handle:    mockHandle,
		config:    cfg,
		stats:     newTestStats(ctrl),
		matched:   true,
		factory:   &anthropicFactory{},
		sseParser: acc,
		model:     "claude-1",
		kind:      KindAnthropic,
	}

	events := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":25,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":10}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	result := filter.OnResponseBody(fake.NewFakeBodyBuffer([]byte(events)), true)
	require.Equal(t, shared.BodyStatusContinue, result)
	require.NotNil(t, filter.llmResp)
}

// --- OnResponseTrailers ---

func TestOnResponseTrailers_HasError_Noop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: true, hasError: true}
	result := filter.OnResponseTrailers(fake.NewFakeHeaderMap(nil))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

func TestOnResponseTrailers_NotMatched_Continue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &llmProxyFilter{handle: mockHandle, config: defaultCfg(), matched: false}
	result := filter.OnResponseTrailers(fake.NewFakeHeaderMap(nil))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

func TestOnResponseTrailers_NonStreaming_ParsesBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedResponseBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(2)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(3)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(5)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(2), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(3), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(5), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  defaultCfg(),
		stats:   newTestStats(ctrl),
		matched: true,
		factory: &openaiFactory{},
		model:   "gpt-4o",
		kind:    KindOpenAI,
	}
	result := filter.OnResponseTrailers(fake.NewFakeHeaderMap(nil))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

// --- custom API type ---

func TestCustomAPIType_RequestParsedLikeOpenAI(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "kind", "custom").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "model", "my-model").Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "is_stream", false).Times(1)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(nil)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(
		fake.NewFakeBodyBuffer([]byte(`{"model":"my-model","messages":[]}`)),
	).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(false).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "custom", "my-model").Return(shared.MetricsSuccess).Times(1)

	cfg := &llmProxyConfig{
		LLMConfigs:        []llmConfig{{Matcher: prefixMatcher("/custom/v1/chat"), Kind: KindCustom}},
		MetadataNamespace: defaultMetadataNamespace,
	}
	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  cfg,
		stats:   newTestStats(ctrl),
		matched: true,
		kind:    KindCustom,
		factory: &customFactory{},
	}
	filter.parseRequestBody()
	require.NotNil(t, filter.llmReq)
}

func TestMatchRule_SuffixMatcher(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{{Matcher: pkg.StringMatcher{Suffix: "/completions"}, Kind: KindOpenAI}},
	}
	f := &llmProxyFilter{config: cfg}
	require.NotNil(t, f.matchRule("/v1/chat/completions"))
	require.Nil(t, f.matchRule("/v1/messages"))
}

func TestMatchRule_RegexMatcher(t *testing.T) {
	m := pkg.StringMatcher{Regex: "^/v1/(chat/completions|messages)$"}
	require.NoError(t, m.ValidateAndParse())
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{{Matcher: m, Kind: KindOpenAI}},
	}
	f := &llmProxyFilter{config: cfg}
	require.NotNil(t, f.matchRule("/v1/chat/completions"))
	require.NotNil(t, f.matchRule("/v1/messages"))
	require.Nil(t, f.matchRule("/v1/other"))
}

// --- llmProxyConfig.ValidateAndParse ---

func TestValidateAndParse_DuplicateOpenAIKind_Error(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{
			{Matcher: pkg.StringMatcher{Prefix: "/a"}, Kind: KindOpenAI},
			{Matcher: pkg.StringMatcher{Prefix: "/b"}, Kind: KindOpenAI},
		},
		MetadataNamespace: defaultMetadataNamespace,
	}
	err := cfg.ValidateAndParse()
	require.Error(t, err)
	require.Contains(t, err.Error(), "openai")
}

func TestValidateAndParse_DuplicateAnthropicKind_Error(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{
			{Matcher: pkg.StringMatcher{Prefix: "/a"}, Kind: KindAnthropic},
			{Matcher: pkg.StringMatcher{Prefix: "/b"}, Kind: KindAnthropic},
		},
		MetadataNamespace: defaultMetadataNamespace,
	}
	err := cfg.ValidateAndParse()
	require.Error(t, err)
	require.Contains(t, err.Error(), "anthropic")
}

func TestValidateAndParse_UnsupportedKind_Error(t *testing.T) {
	cfg := &llmProxyConfig{
		LLMConfigs: []llmConfig{
			{Matcher: pkg.StringMatcher{Prefix: "/v1"}, Kind: "gemini"},
		},
		MetadataNamespace: defaultMetadataNamespace,
	}
	err := cfg.ValidateAndParse()
	require.Error(t, err)
	require.Contains(t, err.Error(), "gemini")
}

// --- LLMModelHeader config option ---

func TestOnRequestBody_LLMModelHeader_SetsRequestHeader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"model":"gpt-4o","stream":false}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	// The configured header key must be set to the extracted model name.
	mockRequestHeaders := mocks.NewMockHeaderMap(ctrl)
	mockHandle.EXPECT().RequestHeaders().Return(mockRequestHeaders).Times(1)
	mockRequestHeaders.EXPECT().Set("x-llm-model", "gpt-4o").Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	cfg := &llmProxyConfig{
		LLMConfigs:        []llmConfig{{Matcher: prefixMatcher("/v1/chat/completions"), Kind: KindOpenAI, Factory: &openaiFactory{}}},
		MetadataNamespace: defaultMetadataNamespace,
		LLMModelHeader:    "x-llm-model",
	}
	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  cfg,
		stats:   newTestStats(ctrl),
		matched: true,
		kind:    KindOpenAI,
		factory: &openaiFactory{},
	}
	filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
}

// --- ClearRouteCache config option ---

func TestOnRequestBody_ClearRouteCache_CallsClearRouteCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{"model":"gpt-4o","stream":false}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().ClearRouteCache().Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idRequestTotal, uint64(1), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	cfg := &llmProxyConfig{
		LLMConfigs:        []llmConfig{{Matcher: prefixMatcher("/v1/chat/completions"), Kind: KindOpenAI, Factory: &openaiFactory{}}},
		MetadataNamespace: defaultMetadataNamespace,
		ClearRouteCache:   true,
	}
	filter := &llmProxyFilter{
		handle:  mockHandle,
		config:  cfg,
		stats:   newTestStats(ctrl),
		matched: true,
		kind:    KindOpenAI,
		factory: &openaiFactory{},
	}
	filter.OnRequestBody(fake.NewFakeBodyBuffer(body), true)
}

// --- OnResponseTrailers with SSE ---

func TestOnResponseTrailers_Streaming_FinishesSSE(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "input_tokens", uint32(10)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "output_tokens", uint32(5)).Times(1)
	mockHandle.EXPECT().SetMetadata(defaultMetadataNamespace, "total_tokens", uint32(15)).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(15), "openai", "gpt-4o").Return(shared.MetricsSuccess).Times(1)

	acc := newOpenAISSEParser()
	// Feed a usage chunk before the trailers arrive.
	require.NoError(t, acc.Feed([]byte("data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n")))
	require.NoError(t, acc.Feed([]byte("data: [DONE]\n")))

	filter := &llmProxyFilter{
		handle:    mockHandle,
		config:    defaultCfg(),
		stats:     newTestStats(ctrl),
		matched:   true,
		factory:   &openaiFactory{},
		sseParser: acc,
		model:     "gpt-4o",
		kind:      KindOpenAI,
	}
	result := filter.OnResponseTrailers(fake.NewFakeHeaderMap(nil))
	require.Equal(t, shared.TrailersStatusContinue, result)
	require.NotNil(t, filter.llmResp)
}
