// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"encoding/json"
	"io"
	"strconv"
	"testing"

	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	fake "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// newWAFFactory creates a wafPluginFactory from raw directives, encapsulating the
// JSON marshalling.
func newWAFFactory(t *testing.T, ctrl *gomock.Controller, directives []string, mode string) shared.HttpFilterFactory {
	t.Helper()
	config := map[string]interface{}{
		"directives": directives,
		"mode":       mode,
	}
	configBytes, err := json.Marshal(config)
	require.NoError(t, err)

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("waf_tx_total").Return(shared.MetricID(1), shared.MetricsSuccess)
	configHandle.EXPECT().DefineCounter("waf_tx_blocked", "authority", "phase", "rule_id").Return(shared.MetricID(2), shared.MetricsSuccess)

	factory, err := (&wafPluginConfigFactory{}).Create(configHandle, configBytes)
	require.NoError(t, err)
	return factory
}

func Test_DisableWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wafPluginFactory := newWAFFactory(t, ctrl, []string{"SecRuleEngine Off"}, "FULL")

	t.Run("WAF disabled should skip processing", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		assert.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected header status to continue when WAF is disabled")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected body status to continue when WAF is disabled")

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnRequestTrailers(trailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected trailer status to continue when WAF is disabled")

		pluginHandle.EXPECT().RequestHeaders().Return(fakeHeaderMap)
		responseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus = wafPlugin.OnResponseHeaders(responseHeaders, false)
		assert.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected response header status to continue when WAF is disabled")

		responseBodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus = wafPlugin.OnResponseBody(responseBodyBuffer, false)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected response body status to continue when WAF is disabled")

		responseTrailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus = wafPlugin.OnResponseTrailers(responseTrailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected response trailer status to continue when WAF is disabled")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Get source address", func(t *testing.T) {
		// Get source address.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)

		// To simplify the test, we can call the getSourceAddress directly.
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		var address string
		var port int

		// No attribute set, should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString(""), false)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.1", address, "expected default address")
		assert.Equal(t, 80, port, "expected default port")

		// No port should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1"), true)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.1", address, "expected default address")
		assert.Equal(t, 80, port, "expected default port")

		// Invalid port.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("127.0.0.1:xyz"), true)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.1", address, "expected default address")
		assert.Equal(t, 80, port, "expected default port")

		// Valid address and port.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("127.0.0.7:8080"), true)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.7", address, "expected address 127.0.0.7")
		assert.Equal(t, 8080, port, "expected port 8080")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Get request protocol", func(t *testing.T) {
		// Get request protocol.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)

		// To simplify the test, we can call the getRequestProtocol directly.
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		var protocol string

		// No attribute set, should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString(""), false)
		protocol = wafPlugin.getRequestProtocol()
		assert.Equal(t, "HTTP/1.1", protocol, "expected default protocol HTTP/1.1")

		// Attribute set.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/2"), true)
		protocol = wafPlugin.getRequestProtocol()
		assert.Equal(t, "HTTP/2", protocol, "expected protocol HTTP/2")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})
}

func Test_RequestOnlyWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wafPluginFactory := newWAFFactory(t, ctrl, []string{
		"Include @coraza.conf",
		"Include @ftw.conf",
		"Include @crs-setup.conf",
		"Include @owasp_crs/*.conf",
	}, "REQUEST_ONLY")

	t.Run("Header only request", func(t *testing.T) {
		// Header only request.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("127.0.0.1:8080"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, true)
		assert.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected header status to continue for header only request")
		assert.False(t, wafPlugin.isUpgrade, "expected isUpgrade to be false for non-upgrade request")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle request with upgrade", func(t *testing.T) {
		// Handle request with upgrade.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"connection":   {"keep-alive, Upgrade"},
			"upgrade":      {"websocket"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("127.0.0.1:8080"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected header status to continue for upgrade request")
		require.True(t, wafPlugin.isUpgrade, "expected isUpgrade to be true for upgrade request")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected body status to continue for upgrade request")

		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnRequestBody(bodyBuffer2, true)
		require.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected final body status to continue for upgrade request")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle request with body", func(t *testing.T) {
		// Handle request with body.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"POST"},
			":path":        {"/submit"},
			"x-request-id": {"req-67890"},
			"user-agent":   {"ComposerTest/1.0"},
			"content-type": {"application/json"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:8080"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected header status to stop for request with body")
		require.False(t, wafPlugin.isUpgrade, "expected isUpgrade to be false for non-upgrade request")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus,
			"expected body status to stop and buffer for request body")

		// Final body processing.
		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnRequestBody(bodyBuffer2, true)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected no immediate response from WAF for simple request body")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle request with body and trailers", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"POST"},
			":path":        {"/submit"},
			"x-request-id": {"req-54321"},
			"user-agent":   {"ComposerTest/1.0"},
			"content-type": {"application/json"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:8080"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected header status to stop for request with body")
		require.False(t, wafPlugin.isUpgrade, "expected isUpgrade to be false for non-upgrade request")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus,
			"expected body status to stop and buffer for request body")

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnRequestTrailers(trailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected no immediate response from WAF for simple request trailers")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Response should be no-op in request only mode", func(t *testing.T) {
		// Response should be no-op in request only mode.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		assert.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected response headers to be no-op in request only mode")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected response body to be no-op in request only mode")

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnResponseTrailers(trailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected response trailers to be no-op in request only mode")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})
}

func Test_ResponseOnlyWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wafPluginFactory := newWAFFactory(t, ctrl, []string{
		"Include @coraza.conf",
		"Include @ftw.conf",
		"Include @crs-setup.conf",
		"Include @owasp_crs/*.conf",
	}, "RESPONSE_ONLY")

	t.Run("Request should be no-op in response only mode", func(t *testing.T) {
		// Request should be no-op in response only mode.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		assert.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected header status to continue in response only mode")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected body status to continue in response only mode")

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnRequestTrailers(trailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected trailer status to continue in response only mode")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Header only response", func(t *testing.T) {
		// Header only response.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, true)
		assert.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected response header status to continue for header only response")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle response with upgrade", func(t *testing.T) {
		// Handle response with upgrade.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"connection":   {"keep-alive, Upgrade"},
			"upgrade":      {"websocket"},
		})
		pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"101"},
			"content-type": {"application/json"},
			"connection":   {"Upgrade"},
			"upgrade":      {"websocket"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected response header status to continue for upgrade response")
		require.True(t, wafPlugin.isUpgrade, "expected isUpgrade to be true for upgrade response")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected response body status to continue for upgrade response")

		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnResponseBody(bodyBuffer2, true)
		require.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected final response body status to continue for upgrade response")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle response with body", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected response header status to stop for response with body")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus,
			"expected response body status to stop and buffer for response body")

		// Final body processing.
		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnResponseBody(bodyBuffer2, true)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected no immediate response from WAF for simple response body")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle response with body and trailers", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected response header status to stop for response with body")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus,
			"expected response body status to stop and buffer for response body")

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnResponseTrailers(trailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected no immediate response from WAF for simple response trailers")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})

	t.Run("Handle response with SSE", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/events"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"text/event-stream"},
		})
		pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"text/event-stream"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected response header status to continue for SSE response")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte("data: event1\n\n"))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected response body status to continue for SSE response")

		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte("data: event2\n\n"))
		bodyStatus = wafPlugin.OnResponseBody(bodyBuffer2, false)
		require.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected response body status to continue for SSE response")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})
}

func Test_WellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.NotNil(t, factories, "expected non-nil factories map")
	require.Len(t, factories, 1, "expected exactly one factory")

	factory, ok := factories[ExtensionName]
	require.True(t, ok, "expected factory for extension name %q", ExtensionName)
	assert.IsType(t, &wafPluginConfigFactory{}, factory,
		"expected factory to be of type *wafPluginConfigFactory")
}

func Test_FullWaf(t *testing.T) {
	// Full WAF tests can be added here.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wafPluginFactory := newWAFFactory(t, ctrl, []string{
		"Include @coraza.conf",
		"Include @ftw.conf",
		"Include @crs-setup.conf",
		"Include @owasp_crs/*.conf",
	}, "FULL")

	t.Run("Full WAF request and response processing", func(t *testing.T) {
		// Full WAF request and response processing.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		// Request processing.
		fakeRequestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"POST"},
			":path":        {"/submit"},
			"x-request-id": {"req-99999"},
			"user-agent":   {"ComposerTest/1.0"},
			"content-type": {"application/json"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("127.0.0.1:8080"), true)

		headerStatus := wafPlugin.OnRequestHeaders(fakeRequestHeaders, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected request header status to stop in full WAF mode")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"fulltest","value":456}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus,
			"expected request body status to stop and buffer in full WAF mode")

		// Final request body processing.
		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnRequestBody(bodyBuffer2, true)
		assert.Equal(t, shared.BodyStatusContinue, bodyStatus,
			"expected no immediate response from WAF for full request body")

		// Response processing.
		pluginHandle.EXPECT().RequestHeaders().Return(fakeRequestHeaders)
		fakeResponseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus = wafPlugin.OnResponseHeaders(fakeResponseHeaders, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected response header status to stop in full WAF mode")

		responseBodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"fullsuccess"}`))
		bodyStatus = wafPlugin.OnResponseBody(responseBodyBuffer, false)
		require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus,
			"expected response body status to stop and buffer in full WAF mode")

		// Trailers processing.
		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnResponseTrailers(trailers)
		assert.Equal(t, shared.TrailersStatusContinue, trailerStatus,
			"expected no immediate response from WAF for full response trailers")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})
}

func Test_BlockRequestWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Use custom rules that block requests with specific patterns.
	// Rule 100001: block request headers if X-Block-Test header is "block-me" (status 403).
	// Rule 100002: block request body if it contains "malicious-payload" (status 403).
	// Rule 100003: block response body if it contains "leaked-secret" (status 403).
	wafPluginFactory := newWAFFactory(t, ctrl, []string{
		"SecRuleEngine On",
		"SecRequestBodyAccess On",
		"SecResponseBodyAccess On",
		`SecResponseBodyMimeType text/plain application/json`,
		`SecRule REQUEST_HEADERS:X-Block-Test "@streq block-me" "id:100001,phase:1,deny,status:403,msg:'Blocked by test rule'"`,
		`SecAction "id:100010,phase:1,pass,nolog,ctl:forceRequestBodyVariable=on"`,
		`SecRule REQUEST_BODY "@contains malicious-payload" "id:100002,phase:2,deny,status:403,msg:'Blocked request body'"`,
		`SecRule RESPONSE_BODY "@contains leaked-secret" "id:100003,phase:4,deny,status:403,msg:'Blocked response body'"`,
	}, "FULL")

	t.Run("Block request on headers", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

		// Expect block metrics: rule 100001, phase 1 (request headers).
		pluginHandle.EXPECT().IncrementCounterValue(
			shared.MetricID(2), uint64(1),
			"example.com",
			strconv.Itoa(int(ctypes.PhaseRequestHeaders)),
			"100001",
		).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100001)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestHeaders))
		pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_request_headers_blocked")

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-block-header"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
			"x-block-test": {"block-me"},
		})

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, true)
		assert.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected request to be blocked on headers")

		wafPlugin.OnStreamComplete()
	})

	t.Run("Block request on body", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

		// Expect block metrics: rule 100002, phase 2 (request body).
		pluginHandle.EXPECT().IncrementCounterValue(
			shared.MetricID(2), uint64(1),
			"example.com",
			strconv.Itoa(int(ctypes.PhaseRequestBody)),
			"100002",
		).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100002)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestBody))
		pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_request_body_blocked")

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"POST"},
			":path":        {"/submit"},
			"x-request-id": {"req-block-body"},
			"user-agent":   {"ComposerTest/1.0"},
			"content-type": {"application/json"},
		})

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected header status to stop for request with body")

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"data":"malicious-payload"}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, true)
		assert.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus,
			"expected request body to be blocked")

		wafPlugin.OnStreamComplete()
	})

	t.Run("Block response on body", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			pkg.UnsafeBufferFromString("HTTP/1.1"), true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		require.True(t, ok, "failed to cast plugin to wafPlugin")

		// Process request headers first (clean request, no block).
		fakeRequestHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-block-response"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		headerStatus := wafPlugin.OnRequestHeaders(fakeRequestHeaders, true)
		require.Equal(t, shared.HeadersStatusContinue, headerStatus,
			"expected request headers to continue for clean request")

		// Process response headers.
		pluginHandle.EXPECT().RequestHeaders().Return(fakeRequestHeaders)
		fakeResponseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus = wafPlugin.OnResponseHeaders(fakeResponseHeaders, false)
		require.Equal(t, shared.HeadersStatusStop, headerStatus,
			"expected response header status to stop for response with body")

		// Expect block metrics: rule 100003, phase 4 (response body).
		pluginHandle.EXPECT().IncrementCounterValue(
			shared.MetricID(2), uint64(1),
			"example.com",
			strconv.Itoa(int(ctypes.PhaseResponseBody)),
			"100003",
		).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100003)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseResponseBody))
		pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_response_body_blocked")

		responseBody := fake.NewFakeBodyBuffer([]byte(`{"secret":"leaked-secret"}`))
		bodyStatus := wafPlugin.OnResponseBody(responseBody, true)
		assert.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus,
			"expected response body to be blocked")

		wafPlugin.OnStreamComplete()
	})
}

func Test_BlockRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a real metrics instance backed by mock counters.
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("waf_tx_total").Return(shared.MetricID(1), shared.MetricsSuccess)
	configHandle.EXPECT().DefineCounter("waf_tx_blocked", "authority", "phase", "rule_id").Return(shared.MetricID(2), shared.MetricsSuccess)
	m := newMetrics(configHandle)

	t.Run("nil interruption sends 500 and records internal block metric", func(*testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		p := &wafPlugin{
			handle:            pluginHandle,
			metrics:           m,
			metadataNamespace: defaultMetadataNamespace,
			authority:         "example.com",
		}

		// RecordBlockInternal: authority, phase (empty rule_id).
		pluginHandle.EXPECT().IncrementCounterValue(
			shared.MetricID(2), uint64(1),
			"example.com",
			strconv.Itoa(int(ctypes.PhaseRequestBody)),
			"",
		).Return(shared.MetricsSuccess)
		// Only block_phase metadata is set (no block_rule for internal errors).
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestBody))
		pluginHandle.EXPECT().SendLocalResponse(uint32(500), nil, []byte("Blocked by WAF"), "waf_internal_error")

		p.blockRequest(nil, ctypes.PhaseRequestBody, "waf_internal_error")
	})

	t.Run("interruption with zero status defaults to 403", func(*testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		p := &wafPlugin{
			handle:            pluginHandle,
			metrics:           m,
			metadataNamespace: defaultMetadataNamespace,
			authority:         "example.com",
		}

		interruption := &ctypes.Interruption{
			Status: 0,
			RuleID: 12345,
		}

		pluginHandle.EXPECT().IncrementCounterValue(
			shared.MetricID(2), uint64(1),
			"example.com",
			strconv.Itoa(int(ctypes.PhaseRequestHeaders)),
			"12345",
		).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 12345)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestHeaders))
		pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_request_headers_blocked")

		p.blockRequest(interruption, ctypes.PhaseRequestHeaders, "waf_request_headers_blocked")
	})

	t.Run("interruption with explicit status uses that status", func(*testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		p := &wafPlugin{
			handle:            pluginHandle,
			metrics:           m,
			metadataNamespace: defaultMetadataNamespace,
			authority:         "blocked.example.com",
		}

		interruption := &ctypes.Interruption{
			Status: 429,
			RuleID: 99999,
		}

		pluginHandle.EXPECT().IncrementCounterValue(
			shared.MetricID(2), uint64(1),
			"blocked.example.com",
			strconv.Itoa(int(ctypes.PhaseResponseBody)),
			"99999",
		).Return(shared.MetricsSuccess)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 99999)
		pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseResponseBody))
		pluginHandle.EXPECT().SendLocalResponse(uint32(429), nil, []byte("Blocked by WAF"), "waf_response_body_blocked")

		p.blockRequest(interruption, ctypes.PhaseResponseBody, "waf_response_body_blocked")
	})
}

// A rule matching request body is expected to be blocked when SecRequestBodyAccess is On, but not inspected when it is Off.
func Test_SecRequestBodyAccessOff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tc := range []struct {
		name            string
		reqBodyAccessOn bool
		expectedStatus  shared.BodyStatus
	}{
		{
			name:            "SecRequestBodyAccess On: body is blocked",
			reqBodyAccessOn: true,
			expectedStatus:  shared.BodyStatusStopNoBuffer,
		},
		{
			name:            "SecRequestBodyAccess Off: body is not inspected",
			reqBodyAccessOn: false,
			expectedStatus:  shared.BodyStatusContinue,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqBodyAccessDir := "SecRequestBodyAccess Off"
			if tc.reqBodyAccessOn {
				reqBodyAccessDir = "SecRequestBodyAccess On"
			}
			wafPluginFactory := newWAFFactory(t, ctrl, []string{
				"SecRuleEngine On",
				reqBodyAccessDir,
				`SecAction "id:100010,phase:1,pass,nolog,ctl:forceRequestBodyVariable=on"`,
				`SecRule REQUEST_BODY "@contains malicious-payload" "id:100002,phase:2,deny,status:403,msg:'Blocked request body'"`,
			}, "FULL")

			pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
			pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

			if tc.reqBodyAccessOn {
				pluginHandle.EXPECT().IncrementCounterValue(
					shared.MetricID(2), uint64(1), "example.com", strconv.Itoa(int(ctypes.PhaseRequestBody)), "100002",
				).Return(shared.MetricsSuccess)
				pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100002)
				pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestBody))
				pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_request_body_blocked")
			}

			wafPlugin, ok := wafPluginFactory.Create(pluginHandle).(*wafPlugin)
			require.True(t, ok, "failed to cast plugin to wafPlugin")

			fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
				":authority":   {"example.com:8080"},
				":method":      {"POST"},
				":path":        {"/submit"},
				"content-type": {"application/json"},
			})
			require.Equal(t, shared.HeadersStatusStop, wafPlugin.OnRequestHeaders(fakeHeaderMap, false))

			bodyStatus := wafPlugin.OnRequestBody(fake.NewFakeBodyBuffer([]byte(`{"data":"malicious-payload"}`)), true)
			require.Equal(t, tc.expectedStatus, bodyStatus)

			if tc.expectedStatus == shared.BodyStatusContinue {
				// Ensure the body was not buffered at all when SecRequestBodyAccess is Off.
				// This ensures that it is not the WAF logic skipping the inspection, but we optimize the filter behavior to not buffer the body at all when access is off or MIME type does not match.
				reqBodyReader, err := wafPlugin.txContext.RequestBodyReader()
				require.NoError(t, err)
				bodyBytes, err := io.ReadAll(reqBodyReader)
				require.NoError(t, err)
				require.Empty(t, bodyBytes, "expected request body to not be buffered when SecRequestBodyAccess is Off")
			}

			wafPlugin.OnStreamComplete()
		})
	}
}

// Even when SecRequestBodyAccess is Off, phase 2 rules needs to be evaluated.
// This is needed because some rules can match arguments that are populated in phase 1 (e.g., ARGS from the query string) and do not require request body access.
// This is a common pattern in CRS rules.
func Test_Phase2ArgsRuleWithSecRequestBodyAccessOff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tc := range []struct {
		name            string
		reqBodyAccessOn bool
	}{
		{
			name:            "SecRequestBodyAccess On: phase 2 ARGS rule is executed",
			reqBodyAccessOn: true,
		},
		{
			name:            "SecRequestBodyAccess Off: phase 2 ARGS rule is still executed",
			reqBodyAccessOn: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqBodyAccessDir := "SecRequestBodyAccess Off"
			if tc.reqBodyAccessOn {
				reqBodyAccessDir = "SecRequestBodyAccess On"
			}
			wafPluginFactory := newWAFFactory(t, ctrl, []string{
				"SecRuleEngine On",
				reqBodyAccessDir,
				`SecRule ARGS "@contains malicious-payload" "id:100002,phase:2,deny,status:403,msg:'Blocked ARGS'"`,
			}, "FULL")

			pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
			pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

			// ARGS rule fires regardless of SecRequestBodyAccess: both cases block.
			pluginHandle.EXPECT().IncrementCounterValue(
				shared.MetricID(2), uint64(1), "example.com", strconv.Itoa(int(ctypes.PhaseRequestBody)), "100002",
			).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100002)
			pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestBody))
			pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_request_body_blocked")

			wafPlugin, ok := wafPluginFactory.Create(pluginHandle).(*wafPlugin)
			require.True(t, ok, "failed to cast plugin to wafPlugin")

			fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
				":authority": {"example.com:8080"},
				":method":    {"GET"},
				":path":      {"/submit?payload=malicious-payload"},
			})
			require.Equal(t, shared.HeadersStatusStop, wafPlugin.OnRequestHeaders(fakeHeaderMap, false))

			bodyStatus := wafPlugin.OnRequestBody(fake.NewFakeBodyBuffer(nil), true)
			require.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus, "expected ARGS rule to fire in phase 2 regardless of SecRequestBodyAccess")

			wafPlugin.OnStreamComplete()
		})
	}
}

func Test_Phase2RulesOnHeaderOnlyRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testCases := []struct {
		name       string
		directives []string
		headers    map[string][]string
		ruleID     int
	}{
		{
			name: "phase 2 cookie rule executes on header-only request",
			directives: []string{
				"SecRuleEngine On",
				`SecRule REQUEST_COOKIES "@contains malicious-cookie" "id:100201,phase:2,deny,status:403,msg:'Blocked cookie'"`,
			},
			headers: map[string][]string{
				":authority": {"example.com:8080"},
				":method":    {"GET"},
				":path":      {"/"},
				"cookie":     {"session=malicious-cookie"},
			},
			ruleID: 100201,
		},
		{
			name: "phase 2 request header rule executes on header-only request",
			directives: []string{
				"SecRuleEngine On",
				`SecRule REQUEST_HEADERS:test "@contains malicious-header" "id:100202,phase:2,deny,status:403,msg:'Blocked header'"`,
			},
			headers: map[string][]string{
				":authority": {"example.com:8080"},
				":method":    {"GET"},
				":path":      {"/"},
				"test":       {"malicious-header"},
			},
			ruleID: 100202,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wafPluginFactory := newWAFFactory(t, ctrl, tc.directives, "FULL")

			pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
			pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)
			pluginHandle.EXPECT().IncrementCounterValue(
				shared.MetricID(2), uint64(1), "example.com", strconv.Itoa(int(ctypes.PhaseRequestBody)), strconv.Itoa(tc.ruleID),
			).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, tc.ruleID)
			pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseRequestBody))
			pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_request_body_blocked")

			wafPlugin, ok := wafPluginFactory.Create(pluginHandle).(*wafPlugin)
			require.True(t, ok, "failed to cast plugin to wafPlugin")

			headerStatus := wafPlugin.OnRequestHeaders(fake.NewFakeHeaderMap(tc.headers), true)
			require.Equal(t, shared.HeadersStatusStop, headerStatus, "expected phase 2 block on header-only request")

			wafPlugin.OnStreamComplete()
		})
	}
}

func Test_SecResponseBodyAccessOff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tc := range []struct {
		name             string
		respBodyAccessOn bool
		mimeType         string
		expectedStatus   shared.BodyStatus
	}{
		{
			name:             "SecResponseBodyAccess On, matching MIME type: body is blocked",
			respBodyAccessOn: true,
			mimeType:         "application/json",
			expectedStatus:   shared.BodyStatusStopNoBuffer,
		},
		{
			name:             "SecResponseBodyAccess On, non-matching MIME type: body is NOT stored NOR inspected",
			respBodyAccessOn: true,
			mimeType:         "text/plain",
			expectedStatus:   shared.BodyStatusContinue,
		},
		{
			name:             "SecResponseBodyAccess Off: body is not inspected",
			respBodyAccessOn: false,
			mimeType:         "application/json",
			expectedStatus:   shared.BodyStatusContinue,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			respBodyAccessDir := "SecResponseBodyAccess Off"
			if tc.respBodyAccessOn {
				respBodyAccessDir = "SecResponseBodyAccess On"
			}
			wafPluginFactory := newWAFFactory(t, ctrl, []string{
				"SecRuleEngine On",
				respBodyAccessDir,
				"SecResponseBodyMimeType " + tc.mimeType,
				`SecRule RESPONSE_BODY "@contains leaked-secret" "id:100003,phase:4,deny,status:403,msg:'Blocked response body'"`,
			}, "FULL")

			pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
			pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

			if tc.expectedStatus == shared.BodyStatusStopNoBuffer {
				pluginHandle.EXPECT().IncrementCounterValue(
					shared.MetricID(2), uint64(1), "example.com", strconv.Itoa(int(ctypes.PhaseResponseBody)), "100003",
				).Return(shared.MetricsSuccess)
				pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100003)
				pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseResponseBody))
				pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_response_body_blocked")
			}

			wafPlugin, ok := wafPluginFactory.Create(pluginHandle).(*wafPlugin)
			require.True(t, ok, "failed to cast plugin to wafPlugin")

			requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
				":authority": {"example.com:8080"},
				":method":    {"GET"},
				":path":      {"/"},
			})
			require.Equal(t, shared.HeadersStatusContinue, wafPlugin.OnRequestHeaders(requestHeaders, true))

			pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
			require.Equal(t, shared.HeadersStatusStop, wafPlugin.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{
				":status":      {"200"},
				"content-type": {"application/json"},
			}), false))

			bodyStatus := wafPlugin.OnResponseBody(fake.NewFakeBodyBuffer([]byte(`{"secret":"leaked-secret"}`)), true)
			require.Equal(t, tc.expectedStatus, bodyStatus)

			if tc.expectedStatus == shared.BodyStatusContinue {
				// Ensure the body was not buffered at all when SecResponseBodyAccess is Off or MIME type does not match.
				respBodyReader, err := wafPlugin.txContext.ResponseBodyReader()
				require.NoError(t, err)
				bodyBytes, err := io.ReadAll(respBodyReader)
				require.NoError(t, err)
				require.Empty(t, string(bodyBytes), "expected response body to not be buffered when SecResponseBodyAccess is Off or MIME type does not match")
			}
			wafPlugin.OnStreamComplete()
		})
	}
}

// Even when SecResponseBodyAccess is Off, phase 4 rules needs to be evaluated.
func Test_Phase4ResponseHeadersRuleWithSecResponseBodyAccessOff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tc := range []struct {
		name             string
		respBodyAccessOn bool
	}{
		{
			name:             "SecResponseBodyAccess On: phase 4 RESPONSE_HEADERS rule is executed",
			respBodyAccessOn: true,
		},
		{
			name:             "SecResponseBodyAccess Off: phase 4 RESPONSE_HEADERS rule is still executed",
			respBodyAccessOn: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			respBodyAccessDir := "SecResponseBodyAccess Off"
			if tc.respBodyAccessOn {
				respBodyAccessDir = "SecResponseBodyAccess On"
			}
			wafPluginFactory := newWAFFactory(t, ctrl, []string{
				"SecRuleEngine On",
				respBodyAccessDir,
				`SecRule RESPONSE_HEADERS:content-type "@contains application/json" "id:100003,phase:4,deny,status:403,msg:'Blocked response header'"`,
			}, "FULL")

			pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
			pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true)
			pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true)

			// RESPONSE_HEADERS rule fires regardless of SecResponseBodyAccess: both cases block.
			pluginHandle.EXPECT().IncrementCounterValue(
				shared.MetricID(2), uint64(1), "example.com", strconv.Itoa(int(ctypes.PhaseResponseBody)), "100003",
			).Return(shared.MetricsSuccess)
			pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockRule, 100003)
			pluginHandle.EXPECT().SetMetadata("io.builtonenvoy.waf", metadataKeyBlockPhase, int(ctypes.PhaseResponseBody))
			pluginHandle.EXPECT().SendLocalResponse(uint32(403), nil, []byte("Blocked by WAF"), "waf_response_body_blocked")

			wafPlugin, ok := wafPluginFactory.Create(pluginHandle).(*wafPlugin)
			require.True(t, ok, "failed to cast plugin to wafPlugin")

			requestHeaders := fake.NewFakeHeaderMap(map[string][]string{
				":authority": {"example.com:8080"},
				":method":    {"GET"},
				":path":      {"/"},
			})
			require.Equal(t, shared.HeadersStatusContinue, wafPlugin.OnRequestHeaders(requestHeaders, true))

			pluginHandle.EXPECT().RequestHeaders().Return(requestHeaders)
			require.Equal(t, shared.HeadersStatusStop, wafPlugin.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{
				":status":      {"200"},
				"content-type": {"application/json"},
			}), false))

			bodyStatus := wafPlugin.OnResponseBody(fake.NewFakeBodyBuffer(nil), true)
			require.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus, "expected RESPONSE_HEADERS rule to fire in phase 4 regardless of SecResponseBodyAccess")

			wafPlugin.OnStreamComplete()
		})
	}
}
