// Package main implements a bidirectional SOAP/REST bridge as a Go Composer plugin
// for the Built on Envoy (boe) platform.
//
// It operates as an Envoy HTTP filter that transparently converts between SOAP XML
// and REST JSON in both directions:
//   - SOAP to REST: Incoming SOAP XML requests are parsed, converted to JSON, and
//     forwarded to a REST upstream. REST responses are wrapped back into SOAP envelopes.
//   - REST to SOAP: Incoming REST JSON requests are wrapped in SOAP envelopes and
//     forwarded to a SOAP upstream. SOAP responses are unwrapped to JSON.
//
// Mode detection is automatic based on the request's Content-Type header.
// Configuration is optional — sensible defaults are applied for unmapped operations.
//
// Build: go build -buildmode=plugin -o soap-rest.so .
package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// =============================================================================
// Mode detection constants
// =============================================================================

type filterMode int

const (
	modePassthrough filterMode = iota // non-SOAP, non-REST-to-SOAP — pass through
	modeSoapToRest                    // incoming SOAP request → REST upstream
	modeRestToSoap                    // incoming REST request → SOAP upstream
)

// =============================================================================
// SOAP XML structures
// =============================================================================

// soapEnvelope represents a generic SOAP 1.1/1.2 envelope.
type soapEnvelope struct {
	XMLName xml.Name    `xml:"Envelope"`
	Header  *soapHeader `xml:"Header"`
	Body    soapBody    `xml:"Body"`
}

// soapHeader captures the raw inner XML of the <soap:Header> element
// for passthrough or metadata storage.
type soapHeader struct {
	Content []byte `xml:",innerxml"`
}

// soapBody captures the raw inner XML of the <soap:Body> element.
// The Fault field is populated after parsing if a <Fault> is detected.
type soapBody struct {
	Content []byte `xml:",innerxml"`
	Fault   *soapFault
}

// soapFault represents a SOAP 1.1 Fault element with code, string, and detail.
type soapFault struct {
	XMLName     xml.Name `xml:"Fault"`
	FaultCode   string   `xml:"faultcode"`
	FaultString string   `xml:"faultstring"`
	Detail      string   `xml:"detail"`
}

// =============================================================================
// Configuration
// =============================================================================

// operationConfig maps a SOAP operation to REST and vice versa.
type operationConfig struct {
	// REST side
	RestMethod string            `json:"restMethod"` // GET, POST, PUT, DELETE, PATCH
	RestPath   string            `json:"restPath"`   // e.g. "/users/{id}"
	PathParams map[string]string `json:"pathParams"` // XML element → path param mapping

	// SOAP side
	SoapAction   string `json:"soapAction"`   // SOAPAction header value
	SoapEndpoint string `json:"soapEndpoint"` // path to SOAP service e.g. "/ws/UserService"

	// pre-computed for fast path matching
	pathSegments []string // split restPath segments, computed once at config load
}

// defaultsConfig holds fallback values used when an operation is not explicitly
// configured or when a configured operation has missing fields.
type defaultsConfig struct {
	RestMethod     string `json:"restMethod"`     // default REST method (POST)
	RestPathPrefix string `json:"restPathPrefix"` // default REST path prefix ("/api")
	SoapEndpoint   string `json:"soapEndpoint"`   // default SOAP endpoint path ("/ws")
	SoapNamespace  string `json:"soapNamespace"`  // default SOAP namespace URI
}

// filterConfig is the top-level JSON configuration passed via --config.
// It contains named operation mappings and default fallback values.
type filterConfig struct {
	Operations map[string]*operationConfig `json:"operations"` // keyed by operation name
	Defaults   defaultsConfig             `json:"defaults"`
}

// precompute splits path templates into segments once at config load time,
// avoiding repeated allocations during request processing.
func (c *filterConfig) precompute() {
	for _, op := range c.Operations {
		if op.RestPath != "" {
			op.pathSegments = strings.Split(strings.Trim(op.RestPath, "/"), "/")
		}
	}
}

// =============================================================================
// Filter implementation
// =============================================================================

// soapRestFilter is the per-request filter instance. It holds a reference to the
// shared config and maintains per-request state for mode detection and operation tracking.
type soapRestFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *filterConfig

	// per-request state
	mode          filterMode // determined in OnRequestHeaders, used throughout the request lifecycle
	operationName string     // detected SOAP operation name, used to wrap the response
	matchedOp     string     // cached operation name from detectMode to avoid redundant lookup in transform
}

// ---------- Request path ----------

// OnRequestHeaders detects the request mode (SOAP→REST, REST→SOAP, or passthrough)
// and signals Envoy to buffer the body if transformation is needed.
func (f *soapRestFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	f.mode = f.detectMode(headers)

	if f.mode == modePassthrough {
		f.handle.Log(shared.LogLevelDebug, "soap-rest: passthrough (not SOAP or REST-to-SOAP)")
		return shared.HeadersStatusContinue
	}

	f.handle.Log(shared.LogLevelInfo, "soap-rest: detected mode %d, buffering body", f.mode)

	// We need the full body to transform — tell Envoy to buffer.
	if endStream {
		// No body — edge case. For SOAP-to-REST this is unusual; just continue.
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

// OnRequestBody buffers body chunks until endStream, then performs the full
// request transformation (SOAP→REST or REST→SOAP) on the complete body.
func (f *soapRestFilter) OnRequestBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	if f.mode == modePassthrough {
		return shared.BodyStatusContinue
	}

	if !endStream {
		return shared.BodyStatusStopAndBuffer
	}

	// Collect the full buffered body.
	rawBody := f.collectBody(f.handle.BufferedRequestBody(), body)

	switch f.mode {
	case modeSoapToRest:
		f.transformSoapToRestRequest(rawBody)
	case modeRestToSoap:
		f.transformRestToSoapRequest(rawBody)
	}

	return shared.BodyStatusContinue
}

func (f *soapRestFilter) OnRequestTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	return shared.TrailersStatusContinue
}

// ---------- Response path ----------

// OnResponseHeaders signals Envoy to buffer the response body if the request
// was transformed (non-passthrough), so the response can be converted back.
func (f *soapRestFilter) OnResponseHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	if f.mode == modePassthrough {
		return shared.HeadersStatusContinue
	}

	f.handle.Log(shared.LogLevelInfo, "soap-rest: buffering response body for mode %d", f.mode)

	if endStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

// OnResponseBody buffers response chunks until endStream, then performs the
// reverse transformation: wraps REST JSON back into SOAP (for SOAP→REST mode)
// or unwraps SOAP XML to JSON (for REST→SOAP mode).
func (f *soapRestFilter) OnResponseBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	if f.mode == modePassthrough {
		return shared.BodyStatusContinue
	}

	if !endStream {
		return shared.BodyStatusStopAndBuffer
	}

	rawBody := f.collectBody(f.handle.BufferedResponseBody(), body)

	switch f.mode {
	case modeSoapToRest:
		// REST response → wrap back into SOAP envelope for client
		f.transformRestToSoapResponse(rawBody)
	case modeRestToSoap:
		// SOAP response → unwrap to JSON for client
		f.transformSoapToRestResponse(rawBody)
	}

	return shared.BodyStatusContinue
}

func (f *soapRestFilter) OnResponseTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	return shared.TrailersStatusContinue
}

// =============================================================================
// Mode detection
// =============================================================================

// detectMode inspects the Content-Type and headers to determine which transformation
// mode to use. Returns modeSoapToRest for XML content types, modeRestToSoap for JSON
// with an X-Target-SOAPAction header or a config-matched path, or modePassthrough otherwise.
func (f *soapRestFilter) detectMode(headers shared.HeaderMap) filterMode {
	ct := strings.ToLower(headers.GetOne("content-type"))

	// SOAP → REST: content-type is XML-ish (SOAP 1.1 or 1.2)
	if strings.Contains(ct, "text/xml") || strings.Contains(ct, "application/soap+xml") {
		return modeSoapToRest
	}

	// REST → SOAP: JSON with a marker header or a configured SOAP target
	if strings.Contains(ct, "application/json") {
		// Check for explicit marker header
		if soapAction := headers.GetOne("x-target-soapaction"); soapAction != "" {
			return modeRestToSoap
		}
		// Check if the path matches a configured operation's REST path.
		// Cache the result so transformRestToSoapRequest doesn't redo the lookup.
		path := headers.GetOne(":path")
		if op := f.findOperationByRestPath(path); op != "" {
			f.matchedOp = op
			return modeRestToSoap
		}
	}

	return modePassthrough
}

// =============================================================================
// SOAP → REST request transformation
// =============================================================================

// transformSoapToRestRequest parses the SOAP envelope, extracts the operation
// and parameters, converts to JSON, and rewrites the request method/path/body
// to target the REST upstream.
func (f *soapRestFilter) transformSoapToRestRequest(rawBody []byte) {
	headers := f.handle.RequestHeaders()

	// 1. Parse SOAP envelope
	envelope, err := parseSoapEnvelope(rawBody)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "soap-rest: failed to parse SOAP envelope: %s", err.Error())
		f.sendJSONError(400, "invalid SOAP envelope", err.Error())
		return
	}

	// 2. Extract operation name and parameters from <soap:Body>
	opName, params, err := extractOperation(envelope.Body.Content)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "soap-rest: failed to extract operation: %s", err.Error())
		f.sendJSONError(400, "cannot extract SOAP operation", err.Error())
		return
	}

	f.operationName = opName
	f.handle.Log(shared.LogLevelInfo, "soap-rest: SOAP operation=%s", opName)

	// 3. Look up config for this operation
	opCfg := f.getOperationConfig(opName)

	// 4. Determine REST method and path
	method := opCfg.RestMethod
	path := opCfg.RestPath

	// Substitute path parameters from SOAP body elements
	if len(opCfg.PathParams) > 0 {
		for paramName, xmlKey := range opCfg.PathParams {
			if val, ok := params[xmlKey]; ok {
				if s, ok := val.(string); ok {
					path = strings.ReplaceAll(path, "{"+paramName+"}", s)
				}
			}
		}
	}

	// 5. Convert params to JSON
	jsonBody, err := json.Marshal(params)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "soap-rest: JSON marshal error: %s", err.Error())
		f.sendJSONError(500, "internal transformation error", err.Error())
		return
	}

	// 6. Rewrite the request
	headers.Set(":method", method)
	headers.Set(":path", path)
	headers.Set("content-type", "application/json")
	headers.Remove("soapaction")
	headers.Remove("content-length")

	// Store SOAP headers as metadata for potential use
	if envelope.Header != nil && len(envelope.Header.Content) > 0 {
		f.handle.SetMetadata("soap-rest", "soap-headers", string(envelope.Header.Content))
	}

	f.replaceBody(f.handle.BufferedRequestBody(), jsonBody)
	f.handle.ClearRouteCache()

	f.handle.Log(shared.LogLevelInfo, "soap-rest: rewrote to %s %s", method, path)
}

// =============================================================================
// REST → SOAP request transformation
// =============================================================================

// transformRestToSoapRequest parses the JSON body, wraps it in a SOAP envelope,
// and rewrites the request to target the SOAP upstream endpoint with appropriate
// SOAPAction header and text/xml content type.
func (f *soapRestFilter) transformRestToSoapRequest(rawBody []byte) {
	headers := f.handle.RequestHeaders()

	// 1. Determine operation name — use cached match from detectMode if available
	opName := f.matchedOp
	if opName == "" {
		if sa := headers.GetOne("x-target-soapaction"); sa != "" {
			opName = f.findOperationBySoapAction(sa)
			if opName == "" {
				// Use the SOAPAction URI's last segment as operation name
				if idx := strings.LastIndexByte(sa, '/'); idx >= 0 {
					opName = sa[idx+1:]
				} else {
					opName = sa
				}
			}
		}
	}
	if opName == "" {
		path := headers.GetOne(":path")
		opName = f.findOperationByRestPath(path)
	}
	if opName == "" {
		// Derive from path: /api/users → Users
		path := headers.GetOne(":path")
		if idx := strings.LastIndexByte(path, '/'); idx >= 0 && idx < len(path)-1 {
			opName = capitalize(path[idx+1:])
		} else {
			opName = capitalize(strings.Trim(path, "/"))
		}
	}

	f.operationName = opName
	f.handle.Log(shared.LogLevelInfo, "soap-rest: REST→SOAP operation=%s", opName)

	// 2. Parse JSON body
	var params map[string]interface{}
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &params); err != nil {
			f.handle.Log(shared.LogLevelError, "soap-rest: failed to parse JSON body: %s", err.Error())
			f.sendJSONError(400, "invalid JSON body", err.Error())
			return
		}
	}

	// 3. Build SOAP envelope
	opCfg := f.getOperationConfig(opName)
	ns := f.config.Defaults.SoapNamespace
	soapXML := buildSoapEnvelope(opName, ns, params)

	// 4. Determine SOAP endpoint path
	soapEndpoint := opCfg.SoapEndpoint
	if soapEndpoint == "" {
		soapEndpoint = f.config.Defaults.SoapEndpoint
		if soapEndpoint == "" {
			soapEndpoint = "/ws"
		}
	}

	// 5. Determine SOAPAction header
	soapAction := opCfg.SoapAction
	if soapAction == "" && ns != "" {
		soapAction = ns + "/" + opName
	}

	// 6. Rewrite the request
	headers.Set(":method", "POST")
	headers.Set(":path", soapEndpoint)
	headers.Set("content-type", "text/xml; charset=utf-8")
	if soapAction != "" {
		// Pre-size: quotes + action string
		var sb strings.Builder
		sb.Grow(len(soapAction) + 2)
		sb.WriteByte('"')
		sb.WriteString(soapAction)
		sb.WriteByte('"')
		headers.Set("soapaction", sb.String())
	}
	headers.Remove("content-length")

	f.replaceBody(f.handle.BufferedRequestBody(), soapXML)
	f.handle.ClearRouteCache()

	f.handle.Log(shared.LogLevelInfo, "soap-rest: rewrote to POST %s (SOAPAction: %s)", soapEndpoint, soapAction)
}

// =============================================================================
// REST → SOAP response transformation (wrap JSON into SOAP envelope)
// =============================================================================

// transformRestToSoapResponse wraps the upstream's REST JSON response back into
// a SOAP envelope for the client. HTTP errors are converted to SOAP Faults.
func (f *soapRestFilter) transformRestToSoapResponse(rawBody []byte) {
	respHeaders := f.handle.ResponseHeaders()
	statusCode := respHeaders.GetOne(":status")

	// Check for HTTP error → SOAP Fault
	if len(statusCode) > 0 && statusCode[0] != '2' {
		soapFaultBytes := buildSoapFault(statusCode, string(rawBody))
		respHeaders.Set("content-type", "text/xml; charset=utf-8")
		respHeaders.Remove("content-length")
		respHeaders.Set(":status", "500")
		f.replaceBody(f.handle.BufferedResponseBody(), soapFaultBytes)
		f.handle.Log(shared.LogLevelInfo, "soap-rest: response wrapped as SOAP Fault (original status %s)", statusCode)
		return
	}

	// Parse JSON response and wrap in SOAP envelope
	var responseData map[string]interface{}
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &responseData); err != nil {
			// Non-JSON response — wrap raw text
			responseData = map[string]interface{}{"result": string(rawBody)}
		}
	}

	responseName := f.operationName + "Response"
	ns := f.config.Defaults.SoapNamespace
	soapXML := buildSoapEnvelope(responseName, ns, responseData)

	respHeaders.Set("content-type", "text/xml; charset=utf-8")
	respHeaders.Remove("content-length")
	f.replaceBody(f.handle.BufferedResponseBody(), soapXML)

	f.handle.Log(shared.LogLevelInfo, "soap-rest: response wrapped as SOAP envelope (%s)", responseName)
}

// =============================================================================
// SOAP → REST response transformation (unwrap SOAP to JSON)
// =============================================================================

// transformSoapToRestResponse unwraps the upstream's SOAP XML response to JSON
// for the client. SOAP Faults are converted to HTTP 500 JSON error responses.
func (f *soapRestFilter) transformSoapToRestResponse(rawBody []byte) {
	respHeaders := f.handle.ResponseHeaders()

	envelope, err := parseSoapEnvelope(rawBody)
	if err != nil {
		// Can't parse as SOAP — pass through as-is
		f.handle.Log(shared.LogLevelWarn, "soap-rest: response is not valid SOAP, passing through: %s", err.Error())
		return
	}

	// Check for SOAP Fault
	if envelope.Body.Fault != nil {
		fault := envelope.Body.Fault
		jsonErr, _ := json.Marshal(map[string]interface{}{
			"error":     fault.FaultString,
			"faultCode": fault.FaultCode,
			"detail":    fault.Detail,
		})
		respHeaders.Set("content-type", "application/json")
		respHeaders.Remove("content-length")
		respHeaders.Set(":status", "500")
		f.replaceBody(f.handle.BufferedResponseBody(), jsonErr)
		f.handle.Log(shared.LogLevelInfo, "soap-rest: SOAP Fault → JSON error")
		return
	}

	// Extract body content and convert XML → JSON
	_, params, err := extractOperation(envelope.Body.Content)
	if err != nil {
		f.handle.Log(shared.LogLevelWarn, "soap-rest: could not extract response operation: %s", err.Error())
		// Fallback: try to convert raw body content
		params = xmlToMap(envelope.Body.Content)
	}

	jsonBody, err := json.Marshal(params)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "soap-rest: failed to marshal response to JSON: %s", err.Error())
		return
	}

	respHeaders.Set("content-type", "application/json")
	respHeaders.Remove("content-length")
	f.replaceBody(f.handle.BufferedResponseBody(), jsonBody)

	f.handle.Log(shared.LogLevelInfo, "soap-rest: SOAP response → JSON")
}

// =============================================================================
// Error helper
// =============================================================================

// sendJSONError sends a local JSON error response. Uses json.Marshal for the
// detail string to ensure proper escaping of quotes, backslashes, and control
// characters that would otherwise produce invalid JSON.
func (f *soapRestFilter) sendJSONError(status int, errorMsg, detail string) {
	// json.Marshal the detail to get a properly escaped JSON string (with quotes)
	escapedDetail, _ := json.Marshal(detail)
	escapedMsg, _ := json.Marshal(errorMsg)

	// Pre-size buffer: {"error":...,"detail":...}
	var buf bytes.Buffer
	buf.Grow(len(escapedMsg) + len(escapedDetail) + 24)
	buf.WriteString(`{"error":`)
	buf.Write(escapedMsg)
	buf.WriteString(`,"detail":`)
	buf.Write(escapedDetail)
	buf.WriteByte('}')
	f.handle.SendLocalResponse(uint32(status), nil, buf.Bytes(), "soap-rest-error")
}

// =============================================================================
// SOAP XML parsing utilities
// =============================================================================

// parseSoapEnvelope parses a SOAP 1.1 or 1.2 envelope from raw XML bytes.
func parseSoapEnvelope(data []byte) (*soapEnvelope, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	// Find the Envelope start element
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return nil, fmt.Errorf("no SOAP Envelope found")
		}
		if err != nil {
			return nil, fmt.Errorf("XML parse error: %w", err)
		}

		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "Envelope" {
				env := &soapEnvelope{}
				if err := decoder.DecodeElement(env, &se); err != nil {
					return nil, fmt.Errorf("failed to decode Envelope: %w", err)
				}

				// Check for SOAP Fault in body
				env.Body.Fault = detectSoapFault(env.Body.Content)

				return env, nil
			}
		}
	}
}

// detectSoapFault checks if the body content contains a SOAP Fault.
func detectSoapFault(bodyContent []byte) *soapFault {
	decoder := xml.NewDecoder(bytes.NewReader(bodyContent))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return nil
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "Fault" {
				fault := &soapFault{}
				if err := decoder.DecodeElement(fault, &se); err != nil {
					return nil
				}
				return fault
			}
		}
	}
}

// extractOperation extracts the first child element of <soap:Body> as the
// operation name, and its children as a parameter map.
func extractOperation(bodyContent []byte) (string, map[string]interface{}, error) {
	decoder := xml.NewDecoder(bytes.NewReader(bodyContent))

	// Find the first element — that's the operation wrapper
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return "", nil, fmt.Errorf("empty SOAP body")
		}
		if err != nil {
			return "", nil, fmt.Errorf("XML parse error in body: %w", err)
		}

		if se, ok := tok.(xml.StartElement); ok {
			opName := se.Name.Local

			// Read the operation element's inner XML
			var innerXML struct {
				Content []byte `xml:",innerxml"`
			}
			if err := decoder.DecodeElement(&innerXML, &se); err != nil {
				return opName, nil, fmt.Errorf("failed to read operation content: %w", err)
			}

			params := xmlToMap(innerXML.Content)
			return opName, params, nil
		}
	}
}

// xmlToMap converts flat or nested XML elements into a map[string]interface{}.
// Repeated elements with the same name become arrays.
// Attributes are stored with "@" prefix.
func xmlToMap(data []byte) map[string]interface{} {
	if len(data) == 0 {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{}, 8) // pre-size for typical element count
	decoder := xml.NewDecoder(bytes.NewReader(data))
	populateMap(decoder, result)
	return result
}

// populateMap reads XML tokens from decoder and populates the given map.
func populateMap(decoder *xml.Decoder, m map[string]interface{}) {
	for {
		tok, err := decoder.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.StartElement:
			key := t.Name.Local

			// Only allocate attribute map if there are attributes
			var attrs map[string]interface{}
			if len(t.Attr) > 0 {
				attrs = make(map[string]interface{}, len(t.Attr))
				for _, attr := range t.Attr {
					attrs["@"+attr.Name.Local] = attr.Value
				}
			}

			childValue := decodeElement(decoder, attrs)

			// Handle repeated elements → array
			if existing, exists := m[key]; exists {
				switch v := existing.(type) {
				case []interface{}:
					m[key] = append(v, childValue)
				default:
					m[key] = []interface{}{v, childValue}
				}
			} else {
				m[key] = childValue
			}

		case xml.EndElement:
			return
		}
	}
}

// decodeElement reads the content of an XML element and returns either a
// string (for leaf text elements) or a map (for elements with children).
func decodeElement(decoder *xml.Decoder, attrs map[string]interface{}) interface{} {
	var textContent strings.Builder
	hasChildren := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			hasChildren = true
			// Lazy-init attrs map if this is the first child and we had no attributes
			if attrs == nil {
				attrs = make(map[string]interface{}, 4)
			}

			key := t.Name.Local
			var childAttrs map[string]interface{}
			if len(t.Attr) > 0 {
				childAttrs = make(map[string]interface{}, len(t.Attr))
				for _, attr := range t.Attr {
					childAttrs["@"+attr.Name.Local] = attr.Value
				}
			}
			childValue := decodeElement(decoder, childAttrs)

			if existing, exists := attrs[key]; exists {
				switch v := existing.(type) {
				case []interface{}:
					attrs[key] = append(v, childValue)
				default:
					attrs[key] = []interface{}{v, childValue}
				}
			} else {
				attrs[key] = childValue
			}

		case xml.CharData:
			textContent.Write(t)

		case xml.EndElement:
			if !hasChildren {
				text := strings.TrimSpace(textContent.String())
				if attrs != nil {
					// Has attributes but also text content
					attrs["#text"] = text
					return attrs
				}
				return text
			}
			return attrs
		}
	}

	if attrs == nil {
		return ""
	}
	return attrs
}

// =============================================================================
// SOAP XML building utilities
// =============================================================================

// xmlEscaper is a shared Replacer for XML text escaping — avoids allocating
// a bytes.Buffer per call.
var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"'", "&apos;",
	`"`, "&quot;",
)

// buildSoapEnvelope creates a SOAP 1.1 envelope XML from an operation name,
// namespace, and a parameter map.
func buildSoapEnvelope(opName, namespace string, params map[string]interface{}) []byte {
	// Estimate buffer size: envelope overhead (~250 bytes) + params (~64 bytes per key)
	estimatedSize := 256 + len(params)*64
	var buf bytes.Buffer
	buf.Grow(estimatedSize)

	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"`)
	if namespace != "" {
		buf.WriteString(` xmlns:ns="`)
		buf.WriteString(namespace)
		buf.WriteByte('"')
	}
	buf.WriteByte('>')
	buf.WriteString(`<soap:Body>`)

	if namespace != "" {
		buf.WriteString(`<ns:`)
		buf.WriteString(opName)
		buf.WriteByte('>')
	} else {
		buf.WriteByte('<')
		buf.WriteString(opName)
		buf.WriteByte('>')
	}

	mapToXML(&buf, params)

	if namespace != "" {
		buf.WriteString(`</ns:`)
		buf.WriteString(opName)
		buf.WriteByte('>')
	} else {
		buf.WriteString(`</`)
		buf.WriteString(opName)
		buf.WriteByte('>')
	}

	buf.WriteString(`</soap:Body>`)
	buf.WriteString(`</soap:Envelope>`)
	return buf.Bytes()
}

// buildSoapFault creates a SOAP 1.1 fault envelope.
func buildSoapFault(httpStatus string, detail string) []byte {
	var buf bytes.Buffer
	buf.Grow(256 + len(detail))

	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">`)
	buf.WriteString(`<soap:Body>`)
	buf.WriteString(`<soap:Fault>`)
	buf.WriteString(`<faultcode>soap:Server</faultcode>`)
	buf.WriteString(`<faultstring>HTTP `)
	xmlEscaper.WriteString(&buf, httpStatus)
	buf.WriteString(` Error</faultstring>`)
	buf.WriteString(`<detail>`)
	xmlEscaper.WriteString(&buf, detail)
	buf.WriteString(`</detail>`)
	buf.WriteString(`</soap:Fault>`)
	buf.WriteString(`</soap:Body>`)
	buf.WriteString(`</soap:Envelope>`)
	return buf.Bytes()
}

// mapToXML recursively converts a map to XML elements.
func mapToXML(buf *bytes.Buffer, m map[string]interface{}) {
	for key, val := range m {
		if (len(key) > 0 && key[0] == '@') || key == "#text" {
			continue // skip attributes and text markers in this pass
		}
		writeValue(buf, key, val)
	}
}

// writeValue writes a single key-value pair as an XML element. Handles strings,
// nested maps (with attributes via "@" prefix and text via "#text"), arrays
// (repeated elements), float64, bool, nil (self-closing), and fallback via fmt.Sprint.
func writeValue(buf *bytes.Buffer, key string, val interface{}) {
	switch v := val.(type) {
	case string:
		buf.WriteByte('<')
		buf.WriteString(key)
		buf.WriteByte('>')
		xmlEscaper.WriteString(buf, v)
		buf.WriteString(`</`)
		buf.WriteString(key)
		buf.WriteByte('>')

	case map[string]interface{}:
		buf.WriteByte('<')
		buf.WriteString(key)
		// Write attributes
		for k, attrVal := range v {
			if len(k) > 0 && k[0] == '@' {
				buf.WriteByte(' ')
				buf.WriteString(k[1:])
				buf.WriteString(`="`)
				xmlEscaper.WriteString(buf, fmt.Sprint(attrVal))
				buf.WriteByte('"')
			}
		}
		buf.WriteByte('>')
		// Write text content if present
		if text, ok := v["#text"]; ok {
			xmlEscaper.WriteString(buf, fmt.Sprint(text))
		}
		mapToXML(buf, v)
		buf.WriteString(`</`)
		buf.WriteString(key)
		buf.WriteByte('>')

	case []interface{}:
		for _, item := range v {
			writeValue(buf, key, item)
		}

	case float64:
		buf.WriteByte('<')
		buf.WriteString(key)
		buf.WriteByte('>')
		buf.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		buf.WriteString(`</`)
		buf.WriteString(key)
		buf.WriteByte('>')

	case bool:
		buf.WriteByte('<')
		buf.WriteString(key)
		buf.WriteByte('>')
		buf.WriteString(strconv.FormatBool(v))
		buf.WriteString(`</`)
		buf.WriteString(key)
		buf.WriteByte('>')

	case nil:
		buf.WriteByte('<')
		buf.WriteString(key)
		buf.WriteString(`/>`)

	default:
		buf.WriteByte('<')
		buf.WriteString(key)
		buf.WriteByte('>')
		xmlEscaper.WriteString(buf, fmt.Sprint(v))
		buf.WriteString(`</`)
		buf.WriteString(key)
		buf.WriteByte('>')
	}
}

// =============================================================================
// Configuration helpers
// =============================================================================

// getOperationConfig returns the config for a named operation, falling back
// to defaults for missing fields.
func (f *soapRestFilter) getOperationConfig(opName string) *operationConfig {
	if cfg, ok := f.config.Operations[opName]; ok {
		// Only copy if we need to fill defaults
		if cfg.RestMethod != "" && cfg.RestPath != "" && cfg.SoapEndpoint != "" {
			return cfg // fast path: fully configured, no copy needed
		}
		result := *cfg
		if result.RestMethod == "" {
			result.RestMethod = f.config.Defaults.RestMethod
		}
		if result.RestPath == "" {
			result.RestPath = f.config.Defaults.RestPathPrefix + "/" + strings.ToLower(opName)
		}
		if result.SoapEndpoint == "" {
			result.SoapEndpoint = f.config.Defaults.SoapEndpoint
		}
		return &result
	}

	// No specific config — generate defaults
	method := f.config.Defaults.RestMethod
	if method == "" {
		method = "POST"
	}
	prefix := f.config.Defaults.RestPathPrefix
	if prefix == "" {
		prefix = "/api"
	}

	return &operationConfig{
		RestMethod:   method,
		RestPath:     prefix + "/" + strings.ToLower(opName),
		SoapEndpoint: f.config.Defaults.SoapEndpoint,
	}
}

// findOperationByRestPath finds which operation maps to a given REST path.
// Uses pre-computed path segments to avoid allocations during matching.
func (f *soapRestFilter) findOperationByRestPath(path string) string {
	actualSegments := strings.Split(strings.Trim(path, "/"), "/")
	for name, cfg := range f.config.Operations {
		if len(cfg.pathSegments) == 0 {
			continue
		}
		if matchSegments(cfg.pathSegments, actualSegments) {
			return name
		}
	}
	return ""
}

// findOperationBySoapAction finds which operation maps to a given SOAPAction.
func (f *soapRestFilter) findOperationBySoapAction(action string) string {
	// Strip quotes from SOAPAction
	action = strings.Trim(action, `"`)
	for name, cfg := range f.config.Operations {
		if cfg.SoapAction == action {
			return name
		}
	}
	return ""
}

// matchSegments compares pre-split path template segments against actual path segments.
func matchSegments(template, actual []string) bool {
	if len(template) != len(actual) {
		return false
	}
	for i := range template {
		seg := template[i]
		if len(seg) > 1 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			continue // wildcard segment
		}
		if seg != actual[i] {
			return false
		}
	}
	return true
}

// capitalize returns a string with the first letter uppercased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// =============================================================================
// Body manipulation helpers
// =============================================================================

// collectBody assembles the full body from buffered chunks and the current body buffer.
// Pre-allocates based on known sizes to avoid repeated slice growth.
func (f *soapRestFilter) collectBody(buffered shared.BodyBuffer, current shared.BodyBuffer) []byte {
	totalSize := uint64(0)
	if buffered != nil {
		totalSize += buffered.GetSize()
	}
	if current != nil {
		totalSize += current.GetSize()
	}
	if totalSize == 0 {
		return nil
	}

	result := make([]byte, 0, totalSize)
	if buffered != nil {
		for _, chunk := range buffered.GetChunks() {
			result = append(result, chunk...)
		}
	}
	if current != nil {
		for _, chunk := range current.GetChunks() {
			result = append(result, chunk...)
		}
	}
	return result
}

// replaceBody drains the existing buffered body and replaces it with new content.
// Guards against nil buffered body to prevent nil-pointer dereference.
func (f *soapRestFilter) replaceBody(buffered shared.BodyBuffer, newBody []byte) {
	if buffered == nil {
		f.handle.Log(shared.LogLevelWarn, "soap-rest: replaceBody called with nil buffer, skipping")
		return
	}
	buffered.Drain(buffered.GetSize())
	buffered.Append(newBody)
}

// =============================================================================
// Factory pattern (Envoy SDK)
// =============================================================================

// soapRestFilterFactory creates per-request filter instances, sharing the
// parsed and pre-computed configuration across all requests.
type soapRestFilterFactory struct {
	config *filterConfig
}

// Create returns a new soapRestFilter for each HTTP request.
func (fac *soapRestFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &soapRestFilter{
		handle: handle,
		config: fac.config,
		mode:   modePassthrough,
	}
}

// soapRestConfigFactory is the top-level factory registered with Composer.
// It parses the JSON config once at startup and produces a soapRestFilterFactory.
type soapRestConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON config, applies defaults, pre-computes path segments,
// and returns a soapRestFilterFactory ready to handle requests.
func (fac *soapRestConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	cfg := &filterConfig{
		Operations: make(map[string]*operationConfig),
		Defaults: defaultsConfig{
			RestMethod:     "POST",
			RestPathPrefix: "/api",
			SoapEndpoint:   "/ws",
		},
	}

	if len(config) > 0 {
		if err := json.Unmarshal(config, cfg); err != nil {
			handle.Log(shared.LogLevelError, "soap-rest: failed to parse config: %s", err.Error())
			return nil, err
		}
	}

	// Apply defaults for missing fields
	if cfg.Defaults.RestMethod == "" {
		cfg.Defaults.RestMethod = "POST"
	}
	if cfg.Defaults.RestPathPrefix == "" {
		cfg.Defaults.RestPathPrefix = "/api"
	}

	// Pre-compute path segments for fast matching during request processing
	cfg.precompute()

	handle.Log(shared.LogLevelInfo, "soap-rest: loaded config with %d operations", len(cfg.Operations))
	return &soapRestFilterFactory{config: cfg}, nil
}

// WellKnownHttpFilterConfigFactories is the entry point looked up by Composer.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{
		"soap-rest": &soapRestConfigFactory{},
	}
}
