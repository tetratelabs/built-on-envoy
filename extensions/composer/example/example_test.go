// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package example

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// allMetrics returns a statsCollector with all metrics enabled for testing.
func allMetrics() *statsCollector {
	return &statsCollector{
		counterMetric:              shared.MetricID(1),
		hasCounterMetric:           true,
		counterMetricWithTags:      shared.MetricID(2),
		hasCounterMetricWithTags:   true,
		gaugeMetric:                shared.MetricID(3),
		hasGaugeMetric:             true,
		gaugeMetricWithTags:        shared.MetricID(4),
		hasGaugeMetricWithTags:     true,
		histogramMetric:            shared.MetricID(5),
		hasHistogramMetric:         true,
		histogramMetricWithTags:    shared.MetricID(6),
		hasHistogramMetricWithTags: true,
	}
}

// noMetrics returns a statsCollector with all metrics disabled.
func noMetrics() *statsCollector {
	return &statsCollector{}
}

func newMockFilterHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return mockHandle
}

func TestPluginConfigFactoryCreate(t *testing.T) {
	t.Run("creates factory with all metrics", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
		mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockConfigHandle.EXPECT().DefineCounter("example_counter").
			Return(shared.MetricID(1), shared.MetricsSuccess)
		mockConfigHandle.EXPECT().DefineCounter("example_counter_with_tags", "tag").
			Return(shared.MetricID(2), shared.MetricsSuccess)
		mockConfigHandle.EXPECT().DefineGauge("example_gauge").
			Return(shared.MetricID(3), shared.MetricsSuccess)
		mockConfigHandle.EXPECT().DefineGauge("example_gauge_with_tags", "tag").
			Return(shared.MetricID(4), shared.MetricsSuccess)
		mockConfigHandle.EXPECT().DefineHistogram("example_histogram").
			Return(shared.MetricID(5), shared.MetricsSuccess)
		mockConfigHandle.EXPECT().DefineHistogram("example_histogram_with_tags", "tag").
			Return(shared.MetricID(6), shared.MetricsSuccess)

		factory := &PluginConfigFactory{}
		result, err := factory.Create(mockConfigHandle, []byte(`{"key":"value"}`))
		require.NoError(t, err)
		require.NotNil(t, result)

		pf, ok := result.(*PluginFactory)
		require.True(t, ok)
		assert.True(t, pf.statsCollector.hasCounterMetric)
		assert.True(t, pf.statsCollector.hasCounterMetricWithTags)
		assert.True(t, pf.statsCollector.hasGaugeMetric)
		assert.True(t, pf.statsCollector.hasGaugeMetricWithTags)
		assert.True(t, pf.statsCollector.hasHistogramMetric)
		assert.True(t, pf.statsCollector.hasHistogramMetricWithTags)
		assert.Equal(t, shared.MetricID(1), pf.statsCollector.counterMetric)
		assert.Equal(t, shared.MetricID(2), pf.statsCollector.counterMetricWithTags)
		assert.Equal(t, shared.MetricID(3), pf.statsCollector.gaugeMetric)
		assert.Equal(t, shared.MetricID(4), pf.statsCollector.gaugeMetricWithTags)
		assert.Equal(t, shared.MetricID(5), pf.statsCollector.histogramMetric)
		assert.Equal(t, shared.MetricID(6), pf.statsCollector.histogramMetricWithTags)
	})

	t.Run("handles metric definition failures", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
		mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockConfigHandle.EXPECT().DefineCounter("example_counter").
			Return(shared.MetricID(0), shared.MetricsResult(1))
		mockConfigHandle.EXPECT().DefineCounter("example_counter_with_tags", "tag").
			Return(shared.MetricID(0), shared.MetricsResult(1))
		mockConfigHandle.EXPECT().DefineGauge("example_gauge").
			Return(shared.MetricID(0), shared.MetricsResult(1))
		mockConfigHandle.EXPECT().DefineGauge("example_gauge_with_tags", "tag").
			Return(shared.MetricID(0), shared.MetricsResult(1))
		mockConfigHandle.EXPECT().DefineHistogram("example_histogram").
			Return(shared.MetricID(0), shared.MetricsResult(1))
		mockConfigHandle.EXPECT().DefineHistogram("example_histogram_with_tags", "tag").
			Return(shared.MetricID(0), shared.MetricsResult(1))

		factory := &PluginConfigFactory{}
		result, err := factory.Create(mockConfigHandle, []byte(`{}`))
		require.NoError(t, err)
		require.NotNil(t, result)

		pf, ok := result.(*PluginFactory)
		require.True(t, ok)
		assert.False(t, pf.statsCollector.hasCounterMetric)
		assert.False(t, pf.statsCollector.hasCounterMetricWithTags)
		assert.False(t, pf.statsCollector.hasGaugeMetric)
		assert.False(t, pf.statsCollector.hasGaugeMetricWithTags)
		assert.False(t, pf.statsCollector.hasHistogramMetric)
		assert.False(t, pf.statsCollector.hasHistogramMetricWithTags)
	})
}

func TestPluginFactoryCreate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := newMockFilterHandle(ctrl)
	stats := allMetrics()
	factory := &PluginFactory{statsCollector: stats}

	filter := factory.Create(mockHandle)
	require.NotNil(t, filter)

	p, ok := filter.(*Plugin)
	require.True(t, ok)
	assert.Equal(t, mockHandle, p.handle)
	assert.Equal(t, stats, p.statsCollector)
}

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Len(t, factories, 1)

	factory, ok := factories[ExtensionName]
	require.True(t, ok)
	assert.IsType(t, &PluginConfigFactory{}, factory)
}

func TestOnRequestHeaders(t *testing.T) {
	t.Run("with all metrics enabled", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		stats := allMetrics()

		// Counter expectations
		mockHandle.EXPECT().IncrementCounterValue(stats.counterMetric, uint64(1)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().IncrementCounterValue(stats.counterMetricWithTags, uint64(1), "tag_value").
			Return(shared.MetricsSuccess)

		// Gauge expectations
		mockHandle.EXPECT().SetGaugeValue(stats.gaugeMetric, uint64(42)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().IncrementGaugeValue(stats.gaugeMetric, uint64(2)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().DecrementGaugeValue(stats.gaugeMetric, uint64(1)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().SetGaugeValue(stats.gaugeMetricWithTags, uint64(84), "tag_value").
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().IncrementGaugeValue(stats.gaugeMetricWithTags, uint64(4), "tag_value").
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().DecrementGaugeValue(stats.gaugeMetricWithTags, uint64(2), "tag_value").
			Return(shared.MetricsSuccess)

		// Histogram expectations
		mockHandle.EXPECT().RecordHistogramValue(stats.histogramMetric, uint64(7)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().RecordHistogramValue(stats.histogramMetric, uint64(14)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().RecordHistogramValue(stats.histogramMetric, uint64(21)).
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().RecordHistogramValue(stats.histogramMetricWithTags, uint64(14), "tag_value").
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().RecordHistogramValue(stats.histogramMetricWithTags, uint64(28), "tag_value").
			Return(shared.MetricsSuccess)
		mockHandle.EXPECT().RecordHistogramValue(stats.histogramMetricWithTags, uint64(42), "tag_value").
			Return(shared.MetricsSuccess)

		// Header and attribute expectations
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("example.com", true)
		mockHandle.EXPECT().SetMetadata("example-namespace", "example-key", "example-value")
		mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-key").Return("example-value", true)
		mockHandle.EXPECT().SetMetadata("example-namespace", "example-number-key", int64(42))
		mockHandle.EXPECT().GetMetadataNumber(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-number-key").Return(float64(42), true)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host": {"example.com"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: stats}
		status := plugin.OnRequestHeaders(headers, true)

		assert.Equal(t, shared.HeadersStatusContinue, status)
		assert.Equal(t, "example-value", headers.GetOne("x-example-request-header"))
	})

	t.Run("without metrics", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("example.com", true)
		mockHandle.EXPECT().SetMetadata("example-namespace", "example-key", "example-value")
		mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-key").Return("example-value", true)
		mockHandle.EXPECT().SetMetadata("example-namespace", "example-number-key", int64(42))
		mockHandle.EXPECT().GetMetadataNumber(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-number-key").Return(float64(42), true)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host": {"example.com"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnRequestHeaders(headers, true)

		assert.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("endStream false returns stop", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("example.com", true)
		mockHandle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockHandle.EXPECT().GetMetadataString(gomock.Any(), gomock.Any(), gomock.Any()).
			Return("example-value", true).AnyTimes()
		mockHandle.EXPECT().GetMetadataNumber(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(float64(42), true).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host": {"example.com"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnRequestHeaders(headers, false)

		assert.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("sends local reply when x-send-local-response is true", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("example.com", true)
		mockHandle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockHandle.EXPECT().GetMetadataString(gomock.Any(), gomock.Any(), gomock.Any()).
			Return("example-value", true).AnyTimes()
		mockHandle.EXPECT().GetMetadataNumber(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(float64(42), true).AnyTimes()
		mockHandle.EXPECT().SendLocalResponse(
			uint32(200), gomock.Nil(), []byte("Local Reply from ExamplePlugin"), "example-plugin")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host":                  {"example.com"},
			"x-send-local-response": {"true"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnRequestHeaders(headers, false)

		assert.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("panics on host header/attribute mismatch", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("different-host.com", true)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host": {"example.com"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		assert.Panics(t, func() {
			plugin.OnRequestHeaders(headers, true)
		})
	})

	t.Run("panics on metadata string mismatch", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("example.com", true)
		mockHandle.EXPECT().SetMetadata("example-namespace", "example-key", "example-value")
		mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-key").Return("wrong-value", true)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host": {"example.com"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		assert.Panics(t, func() {
			plugin.OnRequestHeaders(headers, true)
		})
	})

	t.Run("panics on metadata number mismatch", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).
			Return("example.com", true)
		mockHandle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-key").Return("example-value", true)
		mockHandle.EXPECT().GetMetadataNumber(shared.MetadataSourceTypeDynamic,
			"example-namespace", "example-number-key").Return(float64(99), true)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			"host": {"example.com"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		assert.Panics(t, func() {
			plugin.OnRequestHeaders(headers, true)
		})
	})
}

func TestOnRequestBody(t *testing.T) {
	t.Run("endStream true rewrites body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		// Use empty buffers because FakeBodyBuffer.Drain has a bug when draining
		// the full buffer size (missing return after clearing).
		bufferedBody := fake.NewFakeBodyBuffer(nil)
		body := fake.NewFakeBodyBuffer(nil)
		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			"content-length": {"21"},
			"content-type":   {"application/json"},
		})

		mockHandle.EXPECT().BufferedRequestBody().Return(bufferedBody)
		mockHandle.EXPECT().RequestHeaders().Return(requestHeaders)

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnRequestBody(body, true)

		assert.Equal(t, shared.BodyStatusContinue, status)
		assert.Equal(t, "plain/text", requestHeaders.GetOne("content-type"))
		assert.Empty(t, requestHeaders.GetOne("content-length"))
		// Verify the body was rewritten
		assert.Equal(t, "Modified by ExamplePlugin", string(bufferedBody.Body))
	})

	t.Run("endStream false returns stop and buffer", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		body := fake.NewFakeBodyBuffer([]byte("partial body"))

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnRequestBody(body, false)

		assert.Equal(t, shared.BodyStatusStopAndBuffer, status)
	})
}

func TestOnRequestTrailers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := newMockFilterHandle(ctrl)
	bufferedBody := fake.NewFakeBodyBuffer(nil)
	requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
		"content-length": {"21"},
		"content-type":   {"application/json"},
	})

	mockHandle.EXPECT().BufferedRequestBody().Return(bufferedBody)
	mockHandle.EXPECT().RequestHeaders().Return(requestHeaders)

	trailers := fake.NewFakeHeaderMap(map[string][]string{
		"x-trailer": {"value"},
	})

	plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
	status := plugin.OnRequestTrailers(trailers)

	assert.Equal(t, shared.TrailersStatusContinue, status)
	assert.Equal(t, "plain/text", requestHeaders.GetOne("content-type"))
	assert.Empty(t, requestHeaders.GetOne("content-length"))
}

func TestOnResponseHeaders(t *testing.T) {
	t.Run("sets response header and continues on endStream", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		headers := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnResponseHeaders(headers, true)

		assert.Equal(t, shared.HeadersStatusContinue, status)
		assert.Equal(t, "example-value", headers.GetOne("x-example-response-header"))
	})

	t.Run("endStream false returns stop", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		headers := fake.NewFakeHeaderMap(map[string][]string{
			":status": {"200"},
		})

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnResponseHeaders(headers, false)

		assert.Equal(t, shared.HeadersStatusStop, status)
		assert.Equal(t, "example-value", headers.GetOne("x-example-response-header"))
	})
}

func TestOnResponseBody(t *testing.T) {
	t.Run("endStream true with JSON content type wraps body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		bufferedBody := fake.NewFakeBodyBuffer(nil)
		body := fake.NewFakeBodyBuffer(nil)
		responseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			"content-type":   {"application/json"},
			"content-length": {"100"},
		})

		mockHandle.EXPECT().BufferedResponseBody().Return(bufferedBody).Times(2)
		mockHandle.EXPECT().ResponseHeaders().Return(responseHeaders)

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnResponseBody(body, true)

		assert.Equal(t, shared.BodyStatusContinue, status)
		assert.Empty(t, responseHeaders.GetOne("content-length"))
		// With empty original body, the result should be JSON-wrapped
		assert.Contains(t, string(bufferedBody.Body), `"modified_by":"ExamplePlugin"`)
	})

	t.Run("endStream true with non-JSON content type wraps body as text", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		bufferedBody := fake.NewFakeBodyBuffer(nil)
		body := fake.NewFakeBodyBuffer(nil)
		responseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			"content-type":   {"text/plain"},
			"content-length": {"50"},
		})

		mockHandle.EXPECT().BufferedResponseBody().Return(bufferedBody).Times(2)
		mockHandle.EXPECT().ResponseHeaders().Return(responseHeaders)

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnResponseBody(body, true)

		assert.Equal(t, shared.BodyStatusContinue, status)
		assert.Empty(t, responseHeaders.GetOne("content-length"))
		// With empty original body, the result should be text-wrapped
		assert.Contains(t, string(bufferedBody.Body), "Modified by ExamplePlugin")
		assert.Contains(t, string(bufferedBody.Body), "Original Body:")
	})

	t.Run("endStream false returns stop and buffer", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockHandle := newMockFilterHandle(ctrl)
		body := fake.NewFakeBodyBuffer([]byte("partial response"))

		plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
		status := plugin.OnResponseBody(body, false)

		assert.Equal(t, shared.BodyStatusStopAndBuffer, status)
	})
}

func TestOnResponseTrailers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := newMockFilterHandle(ctrl)
	bufferedBody := fake.NewFakeBodyBuffer(nil)
	responseHeaders := fake.NewFakeHeaderMap(map[string][]string{
		"content-type":   {"text/plain"},
		"content-length": {"13"},
	})

	mockHandle.EXPECT().BufferedResponseBody().Return(bufferedBody).Times(2)
	mockHandle.EXPECT().ResponseHeaders().Return(responseHeaders)

	trailers := fake.NewFakeHeaderMap(map[string][]string{
		"x-trailer": {"value"},
	})

	plugin := &Plugin{handle: mockHandle, statsCollector: noMetrics()}
	status := plugin.OnResponseTrailers(trailers)

	assert.Equal(t, shared.TrailersStatusContinue, status)
	assert.Empty(t, responseHeaders.GetOne("content-length"))
}
