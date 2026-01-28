//go:build !no_example

package example

import (
	"fmt"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

type ExamplePlugin struct {
	shared.EmptyHttpFilter

	statsCollector *statsCollector
	handle         shared.HttpFilterHandle
}

// - Get headers from request and log then.
// - Get single header from request and log it.
// - Get attributes from request and log them.
// - Set metadata string and get them.
// - Set metadata number and get them.
// - Set request header and get it and them log it.
// - Update request body if present.
// - Check x-send-local-reply header and if present send local reply.
func (p *ExamplePlugin) OnRequestHeaders(headers shared.HeaderMap,
	endStream bool) shared.HeadersStatus {
	p.handle.Log(shared.LogLevelInfo, "ExamplePlugin: OnRequestHeaders called")

	if p.statsCollector.hasCounterMetric {
		p.handle.IncrementCounterValue(p.statsCollector.counterMetric, 1)
	}
	if p.statsCollector.hasCounterMetricWithTags {
		p.handle.IncrementCounterValue(p.statsCollector.counterMetricWithTags, 1, "tag_value")
	}
	if p.statsCollector.hasGaugeMetric {
		p.handle.SetGaugeValue(p.statsCollector.gaugeMetric, 42)
		p.handle.IncrementGaugeValue(p.statsCollector.gaugeMetric, 2)
		p.handle.DecrementGaugeValue(p.statsCollector.gaugeMetric, 1)
	}
	if p.statsCollector.hasGaugeMetricWithTags {
		p.handle.SetGaugeValue(p.statsCollector.gaugeMetricWithTags, 84, "tag_value")
		p.handle.IncrementGaugeValue(p.statsCollector.gaugeMetricWithTags, 4, "tag_value")
		p.handle.DecrementGaugeValue(p.statsCollector.gaugeMetricWithTags, 2, "tag_value")
	}
	if p.statsCollector.hasHistogramMetric {
		p.handle.RecordHistogramValue(p.statsCollector.histogramMetric, 7)
		p.handle.RecordHistogramValue(p.statsCollector.histogramMetric, 14)
		p.handle.RecordHistogramValue(p.statsCollector.histogramMetric, 21)
	}
	if p.statsCollector.hasHistogramMetricWithTags {
		p.handle.RecordHistogramValue(p.statsCollector.histogramMetricWithTags, 14, "tag_value")
		p.handle.RecordHistogramValue(p.statsCollector.histogramMetricWithTags, 28, "tag_value")
		p.handle.RecordHistogramValue(p.statsCollector.histogramMetricWithTags, 42, "tag_value")
	}

	// All headers example
	p.handle.Log(shared.LogLevelInfo, "Request Headers: %v", headers.GetAll())

	// Single header and attribute example
	hostHeader := headers.GetOne("host")
	hostAttribute, _ := p.handle.GetAttributeString(shared.AttributeIDRequestHost)
	if hostHeader != hostAttribute {
		panic(fmt.Errorf("host header and attribute should be same but %s vs. %s",
			hostHeader, hostAttribute))
	}
	p.handle.Log(shared.LogLevelInfo, "Host Header: %s", hostHeader)

	// Metadata example
	p.handle.SetMetadata("example-namespace", "example-key", "example-value")
	metadataValue, _ := p.handle.GetMetadataString(shared.MetadataSourceTypeDynamic,
		"example-namespace", "example-key")
	if metadataValue != "example-value" {
		panic(fmt.Errorf("metadata value should be 'example-value' but %s", metadataValue))
	}
	p.handle.Log(shared.LogLevelInfo, "Metadata set and get: %s", metadataValue)
	p.handle.SetMetadata("example-namespace", "example-number-key", int64(42))
	metadataNumberValue, _ := p.handle.GetMetadataNumber(shared.MetadataSourceTypeDynamic,
		"example-namespace", "example-number-key")
	if metadataNumberValue != 42 {
		panic(fmt.Errorf("metadata number value should be 42 but %v", metadataNumberValue))
	}
	p.handle.Log(shared.LogLevelInfo, "Metadata number set and get: %v", metadataNumberValue)

	// Set request header example
	headers.Set("x-example-request-header", "example-value")
	updatedHeader := headers.GetOne("x-example-request-header")
	if updatedHeader != "example-value" {
		panic(fmt.Errorf("updated request header should be 'example-value' but %s", updatedHeader))
	}
	p.handle.Log(shared.LogLevelInfo, "Updated Request Header: %s", updatedHeader)

	if headers.GetOne("x-send-local-response") == "true" {
		p.handle.Log(shared.LogLevelInfo, "Sending local reply as x-send-local-response is true")
		p.handle.SendLocalResponse(200, nil, []byte("Local Reply from ExamplePlugin"), "example-plugin")
		return shared.HeadersStatusStop
	}

	if !endStream {
		return shared.HeadersStatusStop // Wait for body
	}
	return shared.HeadersStatusContinue
}

func (p *ExamplePlugin) OnRequestBody(body shared.BodyBuffer,
	endStream bool) shared.BodyStatus {
	p.handle.Log(shared.LogLevelInfo, "ExamplePlugin: OnRequestBody called with body size: %v/%v",
		body.GetSize(), endStream)

	if endStream {
		p.rewriteRequestBody(body)
		return shared.BodyStatusContinue
	}

	return shared.BodyStatusStopAndBuffer
}

func (p *ExamplePlugin) rewriteRequestBody(receivedBody shared.BodyBuffer) {
	bufferedBody := p.handle.BufferedRequestBody()
	bufferedBody.Drain(bufferedBody.GetSize())
	if receivedBody != nil {
		receivedBody.Drain(receivedBody.GetSize())
	}

	newBody := []byte("Modified by ExamplePlugin")
	bufferedBody.Append(newBody)

	p.handle.Log(shared.LogLevelInfo, "Request body modified to: %s", string(newBody))

	requestHeaders := p.handle.RequestHeaders()
	requestHeaders.Remove("content-length")
	requestHeaders.Set("content-type", "plain/text")
}

func (p *ExamplePlugin) OnRequestTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	p.handle.Log(shared.LogLevelInfo, "ExamplePlugin: OnRequestTrailers called with trailers: %v",
		trailers.GetAll())
	p.rewriteRequestBody(nil)
	return shared.TrailersStatusContinue
}

func (p *ExamplePlugin) OnResponseHeaders(headers shared.HeaderMap,
	endStream bool) shared.HeadersStatus {
	p.handle.Log(shared.LogLevelInfo, "ExamplePlugin: OnResponseHeaders called with headers: %v",
		headers.GetAll())

	// Set response header example
	headers.Set("x-example-response-header", "example-value")
	updatedHeader := headers.GetOne("x-example-response-header")
	if updatedHeader != "example-value" {
		panic(fmt.Errorf("updated response header should be 'example-value' but %s", updatedHeader))
	}
	p.handle.Log(shared.LogLevelInfo, "Updated Response Header: %s", updatedHeader)

	if !endStream {
		return shared.HeadersStatusStop // Wait for body
	}
	return shared.HeadersStatusContinue
}

func (p *ExamplePlugin) rewriteResponseBody(receivedBody shared.BodyBuffer) {
	// Get original response body from buffer
	bufferedBody := p.handle.BufferedResponseBody()

	bufferedBodySize := bufferedBody.GetSize()
	receivedBodySize := uint64(0)
	if receivedBody != nil {
		receivedBodySize = receivedBody.GetSize()
	}

	originalBody := make([]byte, 0, bufferedBodySize+receivedBodySize)
	for _, chunk := range p.handle.BufferedResponseBody().GetChunks() {
		originalBody = append(originalBody, chunk...)
	}
	if receivedBody != nil {
		for _, chunk := range receivedBody.GetChunks() {
			originalBody = append(originalBody, chunk...)
		}
	}

	var newBodyWithOriginal []byte

	responseHeaders := p.handle.ResponseHeaders()
	if strings.Contains(responseHeaders.GetOne("content-type"), "application/json") {
		newBodyWithOriginal = []byte(
			fmt.Sprintf(`{"modified_by":"ExamplePlugin","original_body":%s}`,
				string(originalBody)))
	} else {
		newBodyWithOriginal = []byte(fmt.Sprintf("Modified by ExamplePlugin\nOriginal Body:\n%s",
			string(originalBody)))
	}

	// Drain existing buffered body and append new body
	bufferedBody.Drain(bufferedBodySize)
	if receivedBody != nil {
		receivedBody.Drain(receivedBodySize)
	}
	bufferedBody.Append(newBodyWithOriginal)
	p.handle.Log(shared.LogLevelInfo, "Response body modified to: %s", string(newBodyWithOriginal))
	responseHeaders.Remove("content-length")
}

func (p *ExamplePlugin) OnResponseBody(body shared.BodyBuffer,
	endStream bool) shared.BodyStatus {
	p.handle.Log(shared.LogLevelInfo, "ExamplePlugin: OnResponseBody called with body size: %v/%v",
		body.GetSize(), endStream)

	if endStream {
		p.rewriteResponseBody(body)
		return shared.BodyStatusContinue
	}

	return shared.BodyStatusStopAndBuffer
}

func (p *ExamplePlugin) OnResponseTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	p.handle.Log(shared.LogLevelInfo, "ExamplePlugin: OnResponseTrailers called with trailers: %v",
		trailers.GetAll())
	p.rewriteResponseBody(nil)
	return shared.TrailersStatusContinue
}

type ExamplePluginFactory struct {
	statsCollector *statsCollector
}

func (f *ExamplePluginFactory) Create(
	handle shared.HttpFilterHandle) shared.HttpFilter {
	return &ExamplePlugin{handle: handle, statsCollector: f.statsCollector}
}

type ExamplePluginConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

type statsCollector struct {
	counterMetric              shared.MetricID
	hasCounterMetric           bool
	counterMetricWithTags      shared.MetricID
	hasCounterMetricWithTags   bool
	gaugeMetric                shared.MetricID
	hasGaugeMetric             bool
	gaugeMetricWithTags        shared.MetricID
	hasGaugeMetricWithTags     bool
	histogramMetric            shared.MetricID
	hasHistogramMetric         bool
	histogramMetricWithTags    shared.MetricID
	hasHistogramMetricWithTags bool
}

func (f *ExamplePluginConfigFactory) Create(handle shared.HttpFilterConfigHandle,
	unparsedConfig []byte) (shared.HttpFilterFactory, error) {

	handle.Log(shared.LogLevelInfo, "ExamplePluginConfigFactory: Create called with config: %s", string(unparsedConfig))
	// Example of creating metrics
	stats := &statsCollector{}

	counterMetric, status := handle.DefineCounter("example_counter")
	if status == shared.MetricsSuccess {
		stats.counterMetric = counterMetric
		stats.hasCounterMetric = true
	}
	counterMetricWithTags, status := handle.DefineCounter("example_counter_with_tags", "tag")
	if status == shared.MetricsSuccess {
		stats.counterMetricWithTags = counterMetricWithTags
		stats.hasCounterMetricWithTags = true
	}
	gaugeMetric, status := handle.DefineGauge("example_gauge")
	if status == shared.MetricsSuccess {
		stats.gaugeMetric = gaugeMetric
		stats.hasGaugeMetric = true
	}
	gaugeMetricWithTags, status := handle.DefineGauge("example_gauge_with_tags", "tag")
	if status == shared.MetricsSuccess {
		stats.gaugeMetricWithTags = gaugeMetricWithTags
		stats.hasGaugeMetricWithTags = true
	}
	histogramMetric, status := handle.DefineHistogram("example_histogram")
	if status == shared.MetricsSuccess {
		stats.histogramMetric = histogramMetric
		stats.hasHistogramMetric = true
	}
	histogramMetricWithTags, status := handle.DefineHistogram("example_histogram_with_tags", "tag")
	if status == shared.MetricsSuccess {
		stats.histogramMetricWithTags = histogramMetricWithTags
		stats.hasHistogramMetricWithTags = true
	}

	return &ExamplePluginFactory{statsCollector: stats}, nil
}

var wellKnownHttpFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{
	"example": &ExamplePluginConfigFactory{},
}

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return wellKnownHttpFilterConfigFactories
}
