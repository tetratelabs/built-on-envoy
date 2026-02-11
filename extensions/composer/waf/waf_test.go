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
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	if err != nil {
		t.Fatalf("failed to create WAF plugin factory: %v", err)
	}

	t.Run("WAF disabled should skip processing", func(t *testing.T) {
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Errorf("expected header status to continue when WAF is disabled but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected body status to continue when WAF is disabled but got %v",
				bodyStatus)
		}

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnRequestTrailers(trailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected trailer status to continue when WAF is disabled but got %v",
				trailerStatus)
		}

		pluginHandle.EXPECT().RequestHeaders().Return(fakeHeaderMap)
		responseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus = wafPlugin.OnResponseHeaders(responseHeaders, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Errorf("expected response header status to continue when WAF is disabled but got %v",
				headerStatus)
		}

		responseBodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus = wafPlugin.OnResponseBody(responseBodyBuffer, false)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected response body status to continue when WAF is disabled but got %v",
				bodyStatus)
		}

		responseTrailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus = wafPlugin.OnResponseTrailers(responseTrailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected response trailer status to continue when WAF is disabled but got %v",
				trailerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
	})

	t.Run("Get source address", func(t *testing.T) {
		// Get source address.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)

		// To simplify the test, we can call the getSourceAddress directly.
		wafPlugin, ok := plugin.(*wafPlugin)
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		var address string
		var port int

		// No attribute set, should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return("", false)
		address, port = wafPlugin.getSourceAddress()
		if address != "127.0.0.1" || port != 80 {
			t.Errorf("expected default address but got address %s and port %d", address, port)
		}

		// No port should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1", true)
		address, port = wafPlugin.getSourceAddress()
		if address != "127.0.0.1" || port != 80 {
			t.Errorf("expected default port but got address %s and port %d", address, port)
		}

		// Invalid port.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.1:xyz", true)
		address, port = wafPlugin.getSourceAddress()
		if address != "127.0.0.1" || port != 80 {
			t.Errorf("expected default address but got address %s and port %d", address, port)
		}

		// Valid address and port.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(
			"127.0.0.7:8080", true)
		address, port = wafPlugin.getSourceAddress()
		if address != "127.0.0.7" || port != 8080 {
			t.Errorf("expected address 127.0.0.7 and port 8080 but got address %s and port %d",
				address, port)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
	})

	t.Run("Get request protocol", func(t *testing.T) {
		// Get request protocol.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)

		// To simplify the test, we can call the getRequestProtocol directly.
		wafPlugin, ok := plugin.(*wafPlugin)
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		var protocol string

		// No attribute set, should return default.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return("", false)
		protocol = wafPlugin.getRequestProtocol()
		if protocol != "HTTP/1.1" {
			t.Errorf("expected default protocol HTTP/1.1 but got %s", protocol)
		}

		// Attribute set.
		pluginHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(
			"HTTP/2", true)
		protocol = wafPlugin.getRequestProtocol()
		if protocol != "HTTP/2" {
			t.Errorf("expected protocol HTTP/2 but got %s", protocol)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
	})
}

func Test_RequestOnlyWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := map[string]interface{}{
		"directives": []string{
			"Include @recommended-conf",
			"Include @ftw-conf",
			"Include @crs-setup-conf",
			"Include @owasp_crs/*.conf",
		},
		"mode": "REQUEST_ONLY",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	if err != nil {
		t.Fatalf("failed to create WAF plugin factory: %v", err)
	}

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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, true)
		if headerStatus != shared.HeadersStatusContinue {
			t.Errorf("expected header status to continue for header only request but got %v",
				headerStatus)
		}
		if wafPlugin.isUpgrade {
			t.Errorf("expected isUpgrade to be false for non-upgrade request")
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Fatalf("expected header status to continue for upgrade request but got %v",
				headerStatus)
		}
		if !wafPlugin.isUpgrade {
			t.Fatalf("expected isUpgrade to be true for upgrade request")
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)

		if bodyStatus != shared.BodyStatusContinue {
			t.Fatalf("expected body status to continue for upgrade request but got %v",
				bodyStatus)
		}

		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnRequestBody(bodyBuffer2, true)
		if bodyStatus != shared.BodyStatusContinue {
			t.Fatalf("expected final body status to continue for upgrade request but got %v",
				bodyStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusStop {
			t.Fatalf("expected header status to stop for request with body but got %v",
				headerStatus)
		}
		if wafPlugin.isUpgrade {
			t.Fatalf("expected isUpgrade to be false for non-upgrade request")
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusStopAndBuffer {
			t.Fatalf("expected body status to stop and buffer for request body but got %v",
				bodyStatus)
		}

		// Final body processing.
		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnRequestBody(bodyBuffer2, true)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected no immediate response from WAF for simple request body but got %v",
				bodyStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusStop {
			t.Fatalf("expected header status to stop for request with body but got %v",
				headerStatus)
		}
		if wafPlugin.isUpgrade {
			t.Fatalf("expected isUpgrade to be false for non-upgrade request")
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusStopAndBuffer {
			t.Fatalf("expected body status to stop and buffer for request body but got %v",
				bodyStatus)
		}

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnRequestTrailers(trailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected no immediate response from WAF for simple request trailers but got %v",
				trailerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Errorf("expected response headers to be no-op in request only mode but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected response body to be no-op in request only mode but got %v",
				bodyStatus)
		}

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnResponseTrailers(trailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected response trailers to be no-op in request only mode but got %v",
				trailerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
	})
}

func Test_ResponseOnlyWaf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := map[string]interface{}{
		"directives": []string{
			"Include @recommended-conf",
			"Include @ftw-conf",
			"Include @crs-setup-conf",
			"Include @owasp_crs/*.conf",
		},
		"mode": "RESPONSE_ONLY",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	if err != nil {
		t.Fatalf("failed to create WAF plugin factory: %v", err)
	}

	t.Run("Request should be no-op in response only mode", func(t *testing.T) {
		// Request should be no-op in response only mode.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":authority":   {"example.com:8080"},
			":method":      {"GET"},
			":path":        {"/"},
			"x-request-id": {"req-12345"},
			"user-agent":   {"ComposerTest/1.0"},
			"accept":       {"*/*"},
		})

		headerStatus := wafPlugin.OnRequestHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Errorf("expected header status to continue in response only mode but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"test","value":123}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected body status to continue in response only mode but got %v",
				bodyStatus)
		}

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnRequestTrailers(trailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected trailer status to continue in response only mode but got %v",
				trailerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, true)
		if headerStatus != shared.HeadersStatusContinue {
			t.Errorf("expected response header status to continue for header only response but got %v",
				headerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"101"},
			"content-type": {"application/json"},
			"connection":   {"Upgrade"},
			"upgrade":      {"websocket"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Fatalf("expected response header status to continue for upgrade response but got %v",
				headerStatus)
		}
		if !wafPlugin.isUpgrade {
			t.Fatalf("expected isUpgrade to be true for upgrade response")
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)

		if bodyStatus != shared.BodyStatusContinue {
			t.Fatalf("expected response body status to continue for upgrade response but got %v",
				bodyStatus)
		}

		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnResponseBody(bodyBuffer2, true)
		if bodyStatus != shared.BodyStatusContinue {
			t.Fatalf("expected final response body status to continue for upgrade response but got %v",
				bodyStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusStop {
			t.Fatalf("expected response header status to stop for response with body but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusStopAndBuffer {
			t.Fatalf("expected response body status to stop and buffer for response body but got %v",
				bodyStatus)
		}

		// Final body processing.
		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnResponseBody(bodyBuffer2, true)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected no immediate response from WAF for simple response body but got %v",
				bodyStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusStop {
			t.Fatalf("expected response header status to stop for response with body but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"success"}`))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusStopAndBuffer {
			t.Fatalf("expected response body status to stop and buffer for response body but got %v",
				bodyStatus)
		}

		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnResponseTrailers(trailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected no immediate response from WAF for simple response trailers but got %v",
				trailerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
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
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

		fakeHeaderMap := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"text/event-stream"},
		})
		headerStatus := wafPlugin.OnResponseHeaders(fakeHeaderMap, false)
		if headerStatus != shared.HeadersStatusContinue {
			t.Fatalf("expected response header status to continue for SSE response but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte("data: event1\n\n"))
		bodyStatus := wafPlugin.OnResponseBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusContinue {
			t.Fatalf("expected response body status to continue for SSE response but got %v",
				bodyStatus)
		}

		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte("data: event2\n\n"))
		bodyStatus = wafPlugin.OnResponseBody(bodyBuffer2, false)
		if bodyStatus != shared.BodyStatusContinue {
			t.Fatalf("expected response body status to continue for SSE response but got %v",
				bodyStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
	})
}

func Test_FullWaf(t *testing.T) {
	// Full WAF tests can be added here.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := map[string]interface{}{
		"directives": []string{
			"Include @recommended-conf",
			"Include @ftw-conf",
			"Include @crs-setup-conf",
			"Include @owasp_crs/*.conf",
		},
		"mode": "FULL",
	}

	// convert config to bytes
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	configFactory := wafPluginConfigFactory{}
	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	wafPluginFactory, err := configFactory.Create(configHandle, configBytes)
	if err != nil {
		t.Fatalf("failed to create WAF plugin factory: %v", err)
	}

	t.Run("Full WAF request and response processing", func(t *testing.T) {
		// Full WAF request and response processing.
		pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
		pluginHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		plugin := wafPluginFactory.Create(pluginHandle)
		wafPlugin, ok := plugin.(*wafPlugin)
		if !ok {
			t.Fatalf("failed to cast plugin to wafPlugin")
		}

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
		if headerStatus != shared.HeadersStatusStop {
			t.Fatalf("expected request header status to stop in full WAF mode but got %v",
				headerStatus)
		}

		bodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"name":"fulltest","value":456}`))
		bodyStatus := wafPlugin.OnRequestBody(bodyBuffer, false)
		if bodyStatus != shared.BodyStatusStopAndBuffer {
			t.Fatalf("expected request body status to stop and buffer in full WAF mode but got %v",
				bodyStatus)
		}

		// Final request body processing.
		bodyBuffer2 := fake.NewFakeBodyBuffer([]byte{})
		bodyStatus = wafPlugin.OnRequestBody(bodyBuffer2, true)
		if bodyStatus != shared.BodyStatusContinue {
			t.Errorf("expected no immediate response from WAF for full request body but got %v",
				bodyStatus)
		}

		// Response processing.
		pluginHandle.EXPECT().RequestHeaders().Return(fakeRequestHeaders)
		fakeResponseHeaders := fake.NewFakeHeaderMap(map[string][]string{
			":status":      {"200"},
			"content-type": {"application/json"},
		})
		headerStatus = wafPlugin.OnResponseHeaders(fakeResponseHeaders, false)
		if headerStatus != shared.HeadersStatusStop {
			t.Fatalf("expected response header status to stop in full WAF mode but got %v",
				headerStatus)
		}

		responseBodyBuffer := fake.NewFakeBodyBuffer([]byte(`{"result":"fullsuccess"}`))
		bodyStatus = wafPlugin.OnResponseBody(responseBodyBuffer, false)
		if bodyStatus != shared.BodyStatusStopAndBuffer {
			t.Fatalf("expected response body status to stop and buffer in full WAF mode but got %v",
				bodyStatus)
		}

		// Trailers processing.
		trailers := fake.NewFakeHeaderMap(map[string][]string{
			"grpc-status": {"0"},
		})
		trailerStatus := wafPlugin.OnResponseTrailers(trailers)
		if trailerStatus != shared.TrailersStatusContinue {
			t.Errorf("expected no immediate response from WAF for full response trailers but got %v",
				trailerStatus)
		}

		// Ensure destroy is called.
		wafPlugin.OnDestroy()
	})
}
