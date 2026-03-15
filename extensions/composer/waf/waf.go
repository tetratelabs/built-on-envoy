// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package waf implements the WAF HTTP filter plugin using Coraza.
package waf

import (
	"net"
	"strconv"
	"strings"

	"github.com/corazawaf/coraza/v3"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	waf "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/coraza"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/logger"
)

type wafPluginFactory struct {
	shared.EmptyHttpFilterFactory
	config coraza.WAF
	mode   waf.WAFMode
}

func (f *wafPluginFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &wafPlugin{
		logger: logger.GetLogger(),
		handle: handle,
		config: f.config,
		mode:   f.mode,
	}
}

type wafPluginConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *wafPluginConfigFactory) Create(
	_ shared.HttpFilterConfigHandle,
	unparsedConfig []byte,
) (shared.HttpFilterFactory, error) {
	wafConfig, mode, err := waf.NewWAFConfigFromBytes(unparsedConfig, logger.GetLogger())
	if err != nil {
		return nil, err
	}
	return &wafPluginFactory{config: wafConfig, mode: mode}, nil
}

////////////////////////////////////////////////////////////////////////////////////////////////////

// The plugin struct that implements the actual logic.

type wafPlugin struct {
	shared.EmptyHttpFilter
	logger *logger.Logger
	handle shared.HttpFilterHandle
	config coraza.WAF
	mode   waf.WAFMode

	context               ctypes.Transaction
	protocol              string
	isUpgrade             bool
	isSSE                 bool
	requestBodyProcessed  bool
	responseBodyProcessed bool
}

func (p *wafPlugin) getSourceAddress() (string, int) {
	addressAttr, _ := p.handle.GetAttributeString(shared.AttributeIDSourceAddress)
	address := addressAttr.ToUnsafeString()
	if address == "" {
		p.handle.Log(shared.LogLevelDebug, "No source.address attribute")
		// Use a default value if the attribute is not set.
		return "127.0.0.1", 80
	}

	targetIP, targetPortStr, err := net.SplitHostPort(address)
	if err != nil {
		p.handle.Log(shared.LogLevelDebug, "Invalid source.address attribute format")
		return "127.0.0.1", 80
	}
	targetPort, err := strconv.Atoi(targetPortStr)
	if err != nil {
		p.handle.Log(shared.LogLevelDebug, "Invalid source.address port")
		return "127.0.0.1", 80
	}
	return targetIP, targetPort
}

func (p *wafPlugin) getRequestProtocol() string {
	protocolAttr, _ := p.handle.GetAttributeString(shared.AttributeIDRequestProtocol)
	protocol := protocolAttr.ToString()
	if protocol == "" {
		p.handle.Log(shared.LogLevelDebug, "No request.protocol attribute")
		return "HTTP/1.1"
	}
	return protocol
}

func getServerName(host string) string {
	return strings.Clone(host)
}

func (p *wafPlugin) mayInitializeTransaction(headers shared.HeaderMap) {
	if p.context != nil {
		return
	}
	id := headers.GetOne("x-request-id").ToString()
	p.context = p.config.NewTransactionWithID(id)
	p.isUpgrade = p.checkUpgrade(headers)
}

func (p *wafPlugin) checkUpgrade(headers shared.HeaderMap) bool {
	connectionHeader := headers.GetOne("connection").ToUnsafeString()
	upgrade := headers.GetOne("upgrade").ToUnsafeString()
	return strings.Contains(strings.ToLower(connectionHeader), "upgrade") &&
		upgrade != ""
}

func (p *wafPlugin) checkSSE(headers shared.HeaderMap) bool {
	return strings.Contains(strings.ToLower(headers.GetOne("content-type").ToUnsafeString()),
		"text/event-stream")
}

func (p *wafPlugin) OnRequestHeaders(
	headers shared.HeaderMap,
	endOfStream bool,
) shared.HeadersStatus {
	p.mayInitializeTransaction(headers)

	if p.context.IsRuleEngineOff() || (p.mode == waf.ModeResponseOnly) {
		return shared.HeadersStatusContinue
	}

	srcIP, srcPort := p.getSourceAddress()
	// Destination is not known in this context. Use placeholders.
	dstIP, dstPort := "127.0.0.1", 80

	scheme := headers.GetOne(":scheme").ToString()
	if scheme == "" {
		scheme = "http"
	}
	host := headers.GetOne(":authority").ToString()
	path := headers.GetOne(":path").ToString()
	method := headers.GetOne(":method").ToString()
	uri := scheme + "://" + host + path
	// Save for later use in response processing.
	p.protocol = p.getRequestProtocol()

	// CRS rules tend to expect Host even with HTTP/2
	p.context.AddRequestHeader("Host", host)
	p.context.SetServerName(getServerName(host))
	headerMap := headers.GetAll()
	for _, header := range headerMap {
		p.context.AddRequestHeader(header[0].ToString(), header[1].ToString())
	}

	p.context.ProcessConnection(srcIP, srcPort, dstIP, dstPort)
	p.context.ProcessURI(uri, method, p.protocol)
	interruption := p.context.ProcessRequestHeaders()
	if interruption != nil {
		status := interruption.Status
		if status == 0 {
			status = 403
		}
		p.handle.SendLocalResponse(
			uint32(status), //nolint:gosec // status is validated to be non-zero
			nil,
			[]byte("Request blocked by WAF"),
			"waf_request_headers_blocked",
		)
		return shared.HeadersStatusStop
	}

	// If endOfStream is true or it's an upgrade request, continue the filter chain
	// because we won't buffer the body anyway.
	if endOfStream || p.isUpgrade {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (p *wafPlugin) OnRequestBody(
	body shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
	if p.context.IsRuleEngineOff() || (p.mode == waf.ModeResponseOnly ||
		p.requestBodyProcessed) {
		return shared.BodyStatusContinue
	}

	if !p.context.IsRequestBodyAccessible() {
		if !p.handleRequestBody() {
			return shared.BodyStatusStopNoBuffer
		}
		return shared.BodyStatusContinue
	}

	// Write the body chunks to the WAF and handle possible interruptions.
	if !p.writeRequestBody(body) {
		return shared.BodyStatusStopNoBuffer
	}

	// If endOfStream is true, process the body now.
	if endOfStream {
		if !p.handleRequestBody() {
			return shared.BodyStatusStopNoBuffer
		}
		return shared.BodyStatusContinue
	}

	if p.isUpgrade {
		// In case of upgrade, we cannot buffer the body anyway.
		return shared.BodyStatusContinue
	}

	// In other cases, buffer the body until end of stream.
	return shared.BodyStatusStopAndBuffer
}

func (p *wafPlugin) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if p.context.IsRuleEngineOff() || (p.mode == waf.ModeResponseOnly ||
		p.requestBodyProcessed) {
		return shared.TrailersStatusContinue
	}

	// Has trailers means we never had endOfStream in OnRequestBody. If the body
	// is not yet processed, process it now.
	if !p.handleRequestBody() {
		return shared.TrailersStatusStop
	}
	return shared.TrailersStatusContinue
}

func (p *wafPlugin) OnResponseHeaders(
	headers shared.HeaderMap,
	endOfStream bool,
) shared.HeadersStatus {
	p.mayInitializeTransaction(p.handle.RequestHeaders())
	p.isSSE = p.checkSSE(headers)

	if p.context.IsRuleEngineOff() || (p.mode == waf.ModeRequestOnly) {
		return shared.HeadersStatusContinue
	}

	for _, header := range headers.GetAll() {
		p.context.AddResponseHeader(header[0].ToString(), header[1].ToString())
	}
	if p.protocol == "" {
		p.protocol = p.getRequestProtocol()
	}
	codeStr := headers.GetOne(":status").ToUnsafeString()
	if codeStr == "" {
		codeStr = "500"
	}
	code, err := strconv.Atoi(codeStr)
	if err != nil {
		p.handle.Log(shared.LogLevelInfo, "Invalid response status code: %s", codeStr)
		p.handle.SendLocalResponse(
			500,
			nil,
			[]byte("Internal Server Error"),
			"waf_internal_error",
		)
		return shared.HeadersStatusStop
	}

	interruption := p.context.ProcessResponseHeaders(code, p.protocol)
	if interruption != nil {
		status := interruption.Status
		if status == 0 {
			status = 403
		}
		p.handle.SendLocalResponse(
			uint32(status), //nolint:gosec // status is validated to be non-zero
			nil,
			[]byte("Response blocked by WAF"),
			"waf_response_headers_blocked",
		)
		return shared.HeadersStatusStop
	}

	// If endOfStream is true or it's an upgrade or SSE response, continue the filter chain
	// because we won't buffer the body anyway.
	if endOfStream || p.isUpgrade || p.isSSE {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (p *wafPlugin) OnResponseBody(
	body shared.BodyBuffer,
	endOfStream bool,
) shared.BodyStatus {
	if p.context.IsRuleEngineOff() || (p.mode == waf.ModeRequestOnly) ||
		p.responseBodyProcessed {
		return shared.BodyStatusContinue
	}

	if !p.context.IsResponseBodyAccessible() {
		if !p.handleResponseBody() {
			return shared.BodyStatusStopNoBuffer
		}
		return shared.BodyStatusContinue
	}

	// Write the body chunks to the WAF and handle possible interruptions.
	if !p.writeResponseBody(body) {
		return shared.BodyStatusStopNoBuffer
	}

	// If endOfStream is true, process the body now.
	if endOfStream {
		if !p.handleResponseBody() {
			return shared.BodyStatusStopNoBuffer
		}
		return shared.BodyStatusContinue
	}

	if p.isUpgrade || p.isSSE {
		// In case of upgrade or SSE, we cannot buffer the body anyway.
		return shared.BodyStatusContinue
	}

	// In other cases, buffer the body until end of stream.
	return shared.BodyStatusStopAndBuffer
}

func (p *wafPlugin) OnResponseTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if p.context.IsRuleEngineOff() || (p.mode == waf.ModeRequestOnly) ||
		p.responseBodyProcessed {
		return shared.TrailersStatusContinue
	}

	// Has trailers means we never had endOfStream in OnResponseBody. If the body
	// is not yet processed, process it now.
	if !p.handleResponseBody() {
		return shared.TrailersStatusStop
	}

	return shared.TrailersStatusContinue
}

func (p *wafPlugin) OnStreamComplete() {
	if p.context != nil {
		p.context.ProcessLogging()
		err := p.context.Close()
		if err != nil {
			p.handle.Log(shared.LogLevelDebug, "Failed to close WAF transaction: %v", err.Error())
		}
	}
}

func (p *wafPlugin) writeRequestBody(body shared.BodyBuffer) bool {
	if body == nil {
		return true
	}
	for _, chunk := range body.GetChunks() {
		interruption, _, err := p.context.WriteRequestBody(chunk.ToUnsafeBytes())
		if err != nil {
			p.handle.Log(shared.LogLevelInfo,
				"Failed to write partial request body to WAF: %v", err.Error())
			p.handle.SendLocalResponse(
				500,
				nil,
				[]byte("Internal Server Error"),
				"waf_internal_error",
			)
			return false
		}
		// Write*Body triggers Process*Body if the bodylimit (Sec*BodyLimit) is reached.
		if interruption != nil {
			status := interruption.Status
			if status == 0 {
				status = 403
			}
			p.handle.SendLocalResponse(
				uint32(status), //nolint:gosec // status is validated to be non-zero
				nil,
				[]byte("Request blocked by WAF"),
				"waf_request_body_overflow",
			)
			return false
		}
	}
	return true
}

func (p *wafPlugin) handleRequestBody() bool {
	p.requestBodyProcessed = true

	interruption, err := p.context.ProcessRequestBody()
	if err != nil {
		p.handle.Log(shared.LogLevelInfo, "Failed to process request body in WAF: %v", err.Error())
		p.handle.SendLocalResponse(
			500,
			nil,
			[]byte("Internal Server Error"),
			"waf_internal_error",
		)
		return false
	}
	if interruption != nil {
		status := interruption.Status
		if status == 0 {
			status = 403
		}
		p.handle.SendLocalResponse(
			uint32(status), //nolint:gosec // status is validated to be non-zero
			nil,
			[]byte("Request blocked by WAF"),
			"waf_request_body_blocked",
		)
		return false
	}
	return true
}

func (p *wafPlugin) writeResponseBody(body shared.BodyBuffer) bool {
	if body == nil {
		return true
	}
	for _, chunk := range body.GetChunks() {
		interruption, _, err := p.context.WriteResponseBody(chunk.ToUnsafeBytes())
		if err != nil {
			p.handle.Log(shared.LogLevelInfo, "Failed to write partial response body to WAF: %v", err.Error())
			p.handle.SendLocalResponse(
				500,
				nil,
				[]byte("Internal Server Error"),
				"waf_internal_error",
			)
			return false
		}
		// Write*Body triggers Process*Body if the bodylimit (Sec*BodyLimit) is reached.
		if interruption != nil {
			status := interruption.Status
			if status == 0 {
				status = 403
			}
			p.handle.SendLocalResponse(
				uint32(status), //nolint:gosec // status is validated to be non-zero
				nil,
				[]byte("Response blocked by WAF"),
				"waf_response_body_overflow",
			)
			return false
		}
	}
	return true
}

func (p *wafPlugin) handleResponseBody() bool {
	p.responseBodyProcessed = true

	interruption, err := p.context.ProcessResponseBody()
	if err != nil {
		p.handle.Log(shared.LogLevelInfo, "Failed to process response body in WAF: %v", err.Error())
		p.handle.SendLocalResponse(
			500,
			nil,
			[]byte("Internal Server Error"),
			"waf_internal_error",
		)
		return false
	}
	if interruption != nil {
		status := interruption.Status
		if status == 0 {
			status = 403
		}
		p.handle.SendLocalResponse(
			uint32(status), //nolint:gosec // status is validated to be non-zero
			nil,
			[]byte("Response blocked by WAF"),
			"waf_response_body_blocked",
		)
		return false
	}
	return true
}

// ExtensionName is the name of the extension that will be used in the `run` command to refer to this embedded plugin.
const ExtensionName = "coraza-waf"

var wellKnownHTTPFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{
	ExtensionName: &wafPluginConfigFactory{},
}

// WellKnownHttpFilterConfigFactories returns the map of well-known HTTP filter config factories.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { // nolint:revive
	return wellKnownHTTPFilterConfigFactories
}
