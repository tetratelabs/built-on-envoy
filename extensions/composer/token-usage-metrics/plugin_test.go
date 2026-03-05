// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// testBodyBuffer is a minimal BodyBuffer implementation for tests.
type testBodyBuffer struct{ data []byte }

func (b *testBodyBuffer) GetChunks() [][]byte { return [][]byte{b.data} }
func (b *testBodyBuffer) GetSize() uint64      { return uint64(len(b.data)) }
func (b *testBodyBuffer) Drain(n uint64) {
	if n >= uint64(len(b.data)) {
		b.data = nil
		return
	}
	b.data = b.data[n:]
}
func (b *testBodyBuffer) Append(d []byte) { b.data = append(b.data, d...) }

func defaultMetrics(ctrl *gomock.Controller) (*mocks.MockHttpFilterConfigHandle, *metrics) {
	mockCfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockCfgHandle.EXPECT().
		DefineCounter("llm_token_count_prompt", "model").
		Return(shared.MetricID(1), shared.MetricsSuccess)
	mockCfgHandle.EXPECT().
		DefineCounter("llm_token_count_completion", "model").
		Return(shared.MetricID(2), shared.MetricsSuccess)
	mockCfgHandle.EXPECT().
		DefineCounter("llm_token_count_total", "model").
		Return(shared.MetricID(3), shared.MetricsSuccess)
	return mockCfgHandle, &metrics{
		promptTokens:        shared.MetricID(1),
		hasPromptTokens:     true,
		completionTokens:    shared.MetricID(2),
		hasCompletionTokens: true,
		totalTokens:         shared.MetricID(3),
		hasTotalTokens:      true,
	}
}

// --- Tests for pluginConfigFactory.Create ---

func TestPluginConfigFactory_Create_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle, _ := defaultMetrics(ctrl)

	factory := &pluginConfigFactory{}
	ff, err := factory.Create(mockCfgHandle, []byte{})
	require.NoError(t, err)
	require.NotNil(t, ff)

	pf, ok := ff.(*pluginFactory)
	require.True(t, ok)
	require.Equal(t, defaultMetadataNamespace, pf.config.MetadataNamespace)
}

func TestPluginConfigFactory_Create_NilConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle, _ := defaultMetrics(ctrl)

	factory := &pluginConfigFactory{}
	ff, err := factory.Create(mockCfgHandle, nil)
	require.NoError(t, err)

	pf, ok := ff.(*pluginFactory)
	require.True(t, ok)
	require.Equal(t, defaultMetadataNamespace, pf.config.MetadataNamespace)
}

func TestPluginConfigFactory_Create_WithNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle, _ := defaultMetrics(ctrl)

	factory := &pluginConfigFactory{}
	ff, err := factory.Create(mockCfgHandle, []byte(`{"metadata_namespace":"my-ns"}`))
	require.NoError(t, err)

	pf, ok := ff.(*pluginFactory)
	require.True(t, ok)
	require.Equal(t, "my-ns", pf.config.MetadataNamespace)
}

func TestPluginConfigFactory_Create_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockCfgHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any()).Times(1)

	factory := &pluginConfigFactory{}
	ff, err := factory.Create(mockCfgHandle, []byte(`{invalid}`))
	require.Error(t, err)
	require.Nil(t, ff)
}

// --- Tests for pluginFactory.Create ---

func TestPluginFactory_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	m := &metrics{
		promptTokens:    shared.MetricID(1),
		hasPromptTokens: true,
	}
	cfg := &pluginConfig{MetadataNamespace: defaultMetadataNamespace}
	factory := &pluginFactory{config: cfg, metrics: m}

	p := factory.Create(mockHandle)
	require.NotNil(t, p)

	pl, ok := p.(*plugin)
	require.True(t, ok)
	require.Equal(t, mockHandle, pl.handle)
	require.Equal(t, cfg, pl.config)
	require.Equal(t, m, pl.metrics)
}

// --- Tests for WellKnownHttpFilterConfigFactories ---

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Len(t, factories, 1)
	require.Contains(t, factories, ExtensionName)
	_, ok := factories[ExtensionName].(*pluginConfigFactory)
	require.True(t, ok)
}

// --- Tests for plugin.OnResponseBody ---

func TestOnResponseBody_NotEndOfStream_NoMetadataRead(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	// No metadata calls expected when endOfStream is false.

	p := &plugin{
		handle:  mockHandle,
		config:  &pluginConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: &metrics{hasPromptTokens: true, hasCompletionTokens: true, hasTotalTokens: true},
	}
	result := p.OnResponseBody(&testBodyBuffer{}, false)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_EndOfStream_NoTokenMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().
		GetMetadataString(shared.MetadataSourceTypeDynamic, "openai", "llm.model_name").
		Return("", false)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.prompt").
		Return(0.0, false)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.completion").
		Return(0.0, false)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.total").
		Return(0.0, false)
	// No IncrementCounterValue calls expected when metadata is absent.

	p := &plugin{
		handle:  mockHandle,
		config:  &pluginConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: &metrics{promptTokens: 1, hasPromptTokens: true, completionTokens: 2, hasCompletionTokens: true, totalTokens: 3, hasTotalTokens: true},
	}
	result := p.OnResponseBody(&testBodyBuffer{}, true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_EndOfStream_RecordsTokenCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().
		GetMetadataString(shared.MetadataSourceTypeDynamic, "openai", "llm.model_name").
		Return("gpt-4o", true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.prompt").
		Return(20.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.completion").
		Return(10.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.total").
		Return(30.0, true)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(1), uint64(20), "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(2), uint64(10), "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(3), uint64(30), "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)

	p := &plugin{
		handle: mockHandle,
		config: &pluginConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: &metrics{
			promptTokens:        shared.MetricID(1),
			hasPromptTokens:     true,
			completionTokens:    shared.MetricID(2),
			hasCompletionTokens: true,
			totalTokens:         shared.MetricID(3),
			hasTotalTokens:      true,
		},
	}
	result := p.OnResponseBody(&testBodyBuffer{}, true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_EndOfStream_CustomNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().
		GetMetadataString(shared.MetadataSourceTypeDynamic, "my-ns", "llm.model_name").
		Return("claude-3", true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "my-ns", "llm.token_count.prompt").
		Return(50.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "my-ns", "llm.token_count.completion").
		Return(25.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "my-ns", "llm.token_count.total").
		Return(75.0, true)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(1), uint64(50), "claude-3").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(2), uint64(25), "claude-3").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(3), uint64(75), "claude-3").
		Return(shared.MetricsSuccess).Times(1)

	p := &plugin{
		handle: mockHandle,
		config: &pluginConfig{MetadataNamespace: "my-ns"},
		metrics: &metrics{
			promptTokens:        shared.MetricID(1),
			hasPromptTokens:     true,
			completionTokens:    shared.MetricID(2),
			hasCompletionTokens: true,
			totalTokens:         shared.MetricID(3),
			hasTotalTokens:      true,
		},
	}
	result := p.OnResponseBody(&testBodyBuffer{}, true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_EndOfStream_ZeroTokensNotRecorded(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().
		GetMetadataString(shared.MetadataSourceTypeDynamic, "openai", "llm.model_name").
		Return("gpt-4o", true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.prompt").
		Return(0.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.completion").
		Return(0.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.total").
		Return(0.0, true)
	// Zero values must not increment counters.

	p := &plugin{
		handle: mockHandle,
		config: &pluginConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: &metrics{
			promptTokens:        shared.MetricID(1),
			hasPromptTokens:     true,
			completionTokens:    shared.MetricID(2),
			hasCompletionTokens: true,
			totalTokens:         shared.MetricID(3),
			hasTotalTokens:      true,
		},
	}
	result := p.OnResponseBody(&testBodyBuffer{}, true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

// --- Tests for plugin.OnResponseTrailers ---

func TestOnResponseTrailers_RecordsTokenCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().
		GetMetadataString(shared.MetadataSourceTypeDynamic, "openai", "llm.model_name").
		Return("gpt-4o", true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.prompt").
		Return(5.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.completion").
		Return(3.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.total").
		Return(8.0, true)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(1), uint64(5), "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(2), uint64(3), "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(3), uint64(8), "gpt-4o").
		Return(shared.MetricsSuccess).Times(1)

	p := &plugin{
		handle: mockHandle,
		config: &pluginConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: &metrics{
			promptTokens:        shared.MetricID(1),
			hasPromptTokens:     true,
			completionTokens:    shared.MetricID(2),
			hasCompletionTokens: true,
			totalTokens:         shared.MetricID(3),
			hasTotalTokens:      true,
		},
	}
	result := p.OnResponseTrailers(fake.NewFakeHeaderMap(map[string][]string{}))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

func TestOnResponseTrailers_NoModelName_StillRecords(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().
		GetMetadataString(shared.MetadataSourceTypeDynamic, "openai", "llm.model_name").
		Return("", false)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.prompt").
		Return(10.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.completion").
		Return(5.0, true)
	mockHandle.EXPECT().
		GetMetadataNumber(shared.MetadataSourceTypeDynamic, "openai", "llm.token_count.total").
		Return(15.0, true)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(1), uint64(10), "").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(2), uint64(5), "").
		Return(shared.MetricsSuccess).Times(1)
	mockHandle.EXPECT().
		IncrementCounterValue(shared.MetricID(3), uint64(15), "").
		Return(shared.MetricsSuccess).Times(1)

	p := &plugin{
		handle: mockHandle,
		config: &pluginConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: &metrics{
			promptTokens:        shared.MetricID(1),
			hasPromptTokens:     true,
			completionTokens:    shared.MetricID(2),
			hasCompletionTokens: true,
			totalTokens:         shared.MetricID(3),
			hasTotalTokens:      true,
		},
	}
	result := p.OnResponseTrailers(fake.NewFakeHeaderMap(map[string][]string{}))
	require.Equal(t, shared.TrailersStatusContinue, result)
}
