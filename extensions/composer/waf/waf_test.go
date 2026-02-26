// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"encoding/json"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	fake "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func Test_DisableWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := map[string]interface{}{
		"directives": []string{
			"SecRuleEngine Off",
		},
		"mode": "FULL",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	require.NoError(t, err, "failed to marshal config")

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	require.NoError(t, err, "failed to create WAF plugin factory")

	t.Run("WAF disabled should skip processing", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return("", false)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.1", address, "expected default address")
		assert.Equal(t, 80, port, "expected default port")

		// No port should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1", true)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.1", address, "expected default address")
		assert.Equal(t, 80, port, "expected default port")

		// Invalid port.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:xyz", true)
		address, port = wafPlugin.getSourceAddress()
		assert.Equal(t, "127.0.0.1", address, "expected default address")
		assert.Equal(t, 80, port, "expected default port")

		// Valid address and port.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.7:8080", true)
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
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return("", false)
		protocol = wafPlugin.getRequestProtocol()
		assert.Equal(t, "HTTP/1.1", protocol, "expected default protocol HTTP/1.1")

		// Attribute set.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			"HTTP/2", true)
		protocol = wafPlugin.getRequestProtocol()
		assert.Equal(t, "HTTP/2", protocol, "expected protocol HTTP/2")

		// Ensure destroy is called.
		wafPlugin.OnStreamComplete()
	})
}

func Test_RequestOnlyWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := map[string]interface{}{
		"directives": []string{
			"Include @recommended.conf",
			"Include @ftw.conf",
			"Include @crs-setup.conf",
			"Include @owasp_crs/*.conf",
		},
		"mode": "REQUEST_ONLY",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	require.NoError(t, err, "failed to marshal config")

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	require.NoError(t, err, "failed to create WAF plugin factory")

	t.Run("Header only request", func(t *testing.T) {
		// Header only request.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			"HTTP/1.1", true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:8080", true)

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
			"HTTP/1.1", true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:8080", true)

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

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"POST"},
			":path":        {"/submit"},
			"x-request-id": {"req-67890"},
			"user-agent":   {"ComposerTest/1.0"},
			"content-type": {"application/json"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			"HTTP/1.1", true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:8080", true)

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
		// Handle request with body and trailers.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"POST"},
			":path":        {"/submit"},
			"x-request-id": {"req-54321"},
			"user-agent":   {"ComposerTest/1.0"},
			"content-type": {"application/json"},
		})
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			"HTTP/1.1", true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:8080", true)
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

	config := map[string]interface{}{
		"directives": []string{
			"Include @recommended.conf",
			"Include @ftw.conf",
			"Include @crs-setup.conf",
			"Include @owasp_crs/*.conf",
		},
		"mode": "RESPONSE_ONLY",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	require.NoError(t, err, "failed to marshal config")

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	require.NoError(t, err, "failed to create WAF plugin factory")

	t.Run("Request should be no-op in response only mode", func(t *testing.T) {
		// Request should be no-op in response only mode.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
			"HTTP/1.1", true)

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
			"HTTP/1.1", true)

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
		// Handle response with body.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
			"HTTP/1.1", true)

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
		// Handle response with body and trailers.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
			"HTTP/1.1", true)

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
		// Handle response with SSE.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
			"HTTP/1.1", true)

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

	config := map[string]interface{}{
		"directives": []string{
			"Include @recommended.conf",
			"Include @ftw.conf",
			"Include @crs-setup.conf",
			"Include @owasp_crs/*.conf",
		},
		"mode": "FULL",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	require.NoError(t, err, "failed to marshal config")

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	require.NoError(t, err, "failed to create WAF plugin factory")

	t.Run("Full WAF request and response processing", func(t *testing.T) {
		// Full WAF request and response processing.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
			"HTTP/1.1", true)
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:8080", true)

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
