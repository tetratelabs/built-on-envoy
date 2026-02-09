// Unit tests for the soap-rest extension's pure utility functions.
//
// These tests cover XML parsing, XML building, configuration helpers, path matching,
// and JSON escaping safety. Filter callback methods (OnRequestHeaders, OnRequestBody,
// etc.) depend on the Envoy SDK's C bindings and are tested via integration tests
// in test.sh instead.
//
// Run:   go test -v ./...
// Bench: go test -bench=. -benchmem ./...
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// =============================================================================
// parseSoapEnvelope tests
// =============================================================================

func TestParseSoapEnvelope_Valid11(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser><id>42</id></GetUser>
  </soap:Body>
</soap:Envelope>`

	env, err := parseSoapEnvelope([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil envelope")
	}
	if env.Header != nil {
		t.Errorf("expected nil header, got %v", env.Header)
	}
	if len(env.Body.Content) == 0 {
		t.Error("expected non-empty body content")
	}
	if env.Body.Fault != nil {
		t.Error("expected no fault")
	}
}

func TestParseSoapEnvelope_Valid12(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<soap12:Envelope xmlns:soap12="http://www.w3.org/2003/05/soap-envelope">
  <soap12:Body>
    <Ping/>
  </soap12:Body>
</soap12:Envelope>`

	env, err := parseSoapEnvelope([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil envelope")
	}
}

func TestParseSoapEnvelope_WithHeader(t *testing.T) {
	input := `<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">
  <Header><Auth><Token>abc123</Token></Auth></Header>
  <Body><Ping/></Body>
</Envelope>`

	env, err := parseSoapEnvelope([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Header == nil {
		t.Fatal("expected non-nil header")
	}
	if !strings.Contains(string(env.Header.Content), "Token") {
		t.Errorf("expected header to contain Token, got: %s", env.Header.Content)
	}
}

func TestParseSoapEnvelope_WithFault(t *testing.T) {
	input := `<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">
  <Body>
    <Fault>
      <faultcode>soap:Client</faultcode>
      <faultstring>Invalid input</faultstring>
      <detail>Missing required field</detail>
    </Fault>
  </Body>
</Envelope>`

	env, err := parseSoapEnvelope([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Body.Fault == nil {
		t.Fatal("expected SOAP fault")
	}
	if env.Body.Fault.FaultCode != "soap:Client" {
		t.Errorf("expected faultcode 'soap:Client', got '%s'", env.Body.Fault.FaultCode)
	}
	if env.Body.Fault.FaultString != "Invalid input" {
		t.Errorf("expected faultstring 'Invalid input', got '%s'", env.Body.Fault.FaultString)
	}
	if env.Body.Fault.Detail != "Missing required field" {
		t.Errorf("expected detail 'Missing required field', got '%s'", env.Body.Fault.Detail)
	}
}

func TestParseSoapEnvelope_InvalidXML(t *testing.T) {
	_, err := parseSoapEnvelope([]byte("this is not xml"))
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

func TestParseSoapEnvelope_NoEnvelope(t *testing.T) {
	_, err := parseSoapEnvelope([]byte("<root><child/></root>"))
	if err == nil {
		t.Error("expected error when no Envelope element found")
	}
}

func TestParseSoapEnvelope_Empty(t *testing.T) {
	_, err := parseSoapEnvelope([]byte(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

// =============================================================================
// detectSoapFault tests
// =============================================================================

func TestDetectSoapFault_Present(t *testing.T) {
	bodyContent := []byte(`<Fault><faultcode>soap:Server</faultcode><faultstring>Internal error</faultstring><detail>oops</detail></Fault>`)
	fault := detectSoapFault(bodyContent)
	if fault == nil {
		t.Fatal("expected fault to be detected")
	}
	if fault.FaultCode != "soap:Server" {
		t.Errorf("expected faultcode 'soap:Server', got '%s'", fault.FaultCode)
	}
	if fault.FaultString != "Internal error" {
		t.Errorf("expected faultstring 'Internal error', got '%s'", fault.FaultString)
	}
}

func TestDetectSoapFault_Absent(t *testing.T) {
	bodyContent := []byte(`<GetUserResponse><name>Alice</name></GetUserResponse>`)
	fault := detectSoapFault(bodyContent)
	if fault != nil {
		t.Errorf("expected no fault, got %+v", fault)
	}
}

func TestDetectSoapFault_EmptyBody(t *testing.T) {
	fault := detectSoapFault([]byte(""))
	if fault != nil {
		t.Errorf("expected no fault for empty body, got %+v", fault)
	}
}

func TestDetectSoapFault_NamespacePrefixed(t *testing.T) {
	bodyContent := []byte(`<soap:Fault xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><faultcode>soap:Client</faultcode><faultstring>Bad request</faultstring></soap:Fault>`)
	fault := detectSoapFault(bodyContent)
	if fault == nil {
		t.Fatal("expected fault to be detected with namespace prefix")
	}
}

// =============================================================================
// extractOperation tests
// =============================================================================

func TestExtractOperation_Simple(t *testing.T) {
	bodyContent := []byte(`<GetUser><id>42</id><name>Alice</name></GetUser>`)
	opName, params, err := extractOperation(bodyContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opName != "GetUser" {
		t.Errorf("expected 'GetUser', got '%s'", opName)
	}
	if params["id"] != "42" {
		t.Errorf("expected id='42', got '%v'", params["id"])
	}
	if params["name"] != "Alice" {
		t.Errorf("expected name='Alice', got '%v'", params["name"])
	}
}

func TestExtractOperation_Nested(t *testing.T) {
	bodyContent := []byte(`<CreateUser><user><name>Bob</name><age>30</age></user></CreateUser>`)
	opName, params, err := extractOperation(bodyContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opName != "CreateUser" {
		t.Errorf("expected 'CreateUser', got '%s'", opName)
	}
	user, ok := params["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map for 'user', got %T", params["user"])
	}
	if user["name"] != "Bob" {
		t.Errorf("expected name='Bob', got '%v'", user["name"])
	}
	if user["age"] != "30" {
		t.Errorf("expected age='30', got '%v'", user["age"])
	}
}

func TestExtractOperation_EmptyBody(t *testing.T) {
	_, _, err := extractOperation([]byte(""))
	if err == nil {
		t.Error("expected error for empty body")
	}
}

func TestExtractOperation_EmptyElement(t *testing.T) {
	bodyContent := []byte(`<Ping/>`)
	opName, params, err := extractOperation(bodyContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opName != "Ping" {
		t.Errorf("expected 'Ping', got '%s'", opName)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params, got %v", params)
	}
}

func TestExtractOperation_WithNamespace(t *testing.T) {
	bodyContent := []byte(`<ns:GetUser xmlns:ns="http://example.com"><ns:id>1</ns:id></ns:GetUser>`)
	opName, params, err := extractOperation(bodyContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opName != "GetUser" {
		t.Errorf("expected 'GetUser', got '%s'", opName)
	}
	if params["id"] != "1" {
		t.Errorf("expected id='1', got '%v'", params["id"])
	}
}

// =============================================================================
// xmlToMap tests
// =============================================================================

func TestXmlToMap_Simple(t *testing.T) {
	data := []byte(`<name>Alice</name><age>30</age>`)
	m := xmlToMap(data)
	if m["name"] != "Alice" {
		t.Errorf("expected name='Alice', got '%v'", m["name"])
	}
	if m["age"] != "30" {
		t.Errorf("expected age='30', got '%v'", m["age"])
	}
}

func TestXmlToMap_RepeatedElements(t *testing.T) {
	data := []byte(`<item>one</item><item>two</item><item>three</item>`)
	m := xmlToMap(data)
	items, ok := m["item"].([]interface{})
	if !ok {
		t.Fatalf("expected array for 'item', got %T: %v", m["item"], m["item"])
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
	if items[0] != "one" || items[1] != "two" || items[2] != "three" {
		t.Errorf("unexpected items: %v", items)
	}
}

func TestXmlToMap_Nested(t *testing.T) {
	data := []byte(`<user><name>Bob</name><address><city>NYC</city></address></user>`)
	m := xmlToMap(data)
	user, ok := m["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for 'user', got %T", m["user"])
	}
	if user["name"] != "Bob" {
		t.Errorf("expected name='Bob', got '%v'", user["name"])
	}
	addr, ok := user["address"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for 'address', got %T", user["address"])
	}
	if addr["city"] != "NYC" {
		t.Errorf("expected city='NYC', got '%v'", addr["city"])
	}
}

func TestXmlToMap_WithAttributes(t *testing.T) {
	data := []byte(`<user id="42" role="admin"><name>Alice</name></user>`)
	m := xmlToMap(data)
	user, ok := m["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for 'user', got %T", m["user"])
	}
	if user["@id"] != "42" {
		t.Errorf("expected @id='42', got '%v'", user["@id"])
	}
	if user["@role"] != "admin" {
		t.Errorf("expected @role='admin', got '%v'", user["@role"])
	}
	if user["name"] != "Alice" {
		t.Errorf("expected name='Alice', got '%v'", user["name"])
	}
}

func TestXmlToMap_Empty(t *testing.T) {
	m := xmlToMap([]byte(""))
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestXmlToMap_MixedTextAndAttributes(t *testing.T) {
	data := []byte(`<price currency="USD">19.99</price>`)
	m := xmlToMap(data)
	price, ok := m["price"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for 'price', got %T", m["price"])
	}
	if price["@currency"] != "USD" {
		t.Errorf("expected @currency='USD', got '%v'", price["@currency"])
	}
	if price["#text"] != "19.99" {
		t.Errorf("expected #text='19.99', got '%v'", price["#text"])
	}
}

// =============================================================================
// buildSoapEnvelope tests
// =============================================================================

func TestBuildSoapEnvelope_WithNamespace(t *testing.T) {
	params := map[string]interface{}{
		"id":   "42",
		"name": "Alice",
	}
	result := buildSoapEnvelope("GetUser", "http://example.com/services", params)
	s := string(result)

	if !strings.Contains(s, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(s, `xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"`) {
		t.Error("missing SOAP namespace")
	}
	if !strings.Contains(s, `xmlns:ns="http://example.com/services"`) {
		t.Error("missing custom namespace")
	}
	if !strings.Contains(s, `<ns:GetUser>`) {
		t.Error("missing namespaced operation element")
	}
	if !strings.Contains(s, `</ns:GetUser>`) {
		t.Error("missing closing namespaced operation element")
	}
	if !strings.Contains(s, `<soap:Body>`) {
		t.Error("missing SOAP body")
	}
	if !strings.Contains(s, `</soap:Envelope>`) {
		t.Error("missing closing envelope")
	}
}

func TestBuildSoapEnvelope_WithoutNamespace(t *testing.T) {
	params := map[string]interface{}{
		"value": "hello",
	}
	result := buildSoapEnvelope("Ping", "", params)
	s := string(result)

	if strings.Contains(s, `xmlns:ns=`) {
		t.Error("should not have namespace declaration when namespace is empty")
	}
	if !strings.Contains(s, `<Ping>`) {
		t.Error("missing bare operation element")
	}
	if !strings.Contains(s, `<value>hello</value>`) {
		t.Error("missing parameter element")
	}
}

func TestBuildSoapEnvelope_NilParams(t *testing.T) {
	result := buildSoapEnvelope("Ping", "", nil)
	s := string(result)

	if !strings.Contains(s, `<Ping>`) {
		t.Error("missing operation element")
	}
	if !strings.Contains(s, `</Ping>`) {
		t.Error("missing closing operation element")
	}
}

func TestBuildSoapEnvelope_NestedParams(t *testing.T) {
	params := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "Bob",
			"age":  "30",
		},
	}
	result := buildSoapEnvelope("CreateUser", "", params)
	s := string(result)

	if !strings.Contains(s, "<user>") {
		t.Error("missing nested user element")
	}
	if !strings.Contains(s, "<name>Bob</name>") {
		t.Error("missing nested name element")
	}
}

func TestBuildSoapEnvelope_XmlSpecialChars(t *testing.T) {
	params := map[string]interface{}{
		"query": `<script>alert("xss")</script>`,
	}
	result := buildSoapEnvelope("Search", "", params)
	s := string(result)

	if strings.Contains(s, `<script>`) {
		t.Error("XML special characters should be escaped")
	}
	if !strings.Contains(s, `&lt;script&gt;`) {
		t.Error("expected escaped < and >")
	}
}

// =============================================================================
// buildSoapFault tests
// =============================================================================

func TestBuildSoapFault_Basic(t *testing.T) {
	result := buildSoapFault("500", "Internal Server Error")
	s := string(result)

	if !strings.Contains(s, `<soap:Fault>`) {
		t.Error("missing SOAP Fault element")
	}
	if !strings.Contains(s, `<faultcode>soap:Server</faultcode>`) {
		t.Error("missing faultcode")
	}
	if !strings.Contains(s, `<faultstring>HTTP 500 Error</faultstring>`) {
		t.Error("missing faultstring")
	}
	if !strings.Contains(s, `<detail>Internal Server Error</detail>`) {
		t.Error("missing detail")
	}
}

func TestBuildSoapFault_SpecialChars(t *testing.T) {
	result := buildSoapFault("400", `<bad "input" & more>`)
	s := string(result)

	if strings.Contains(s, `<bad`) {
		t.Error("special characters in detail should be escaped")
	}
	if !strings.Contains(s, `&lt;bad`) {
		t.Error("expected < to be escaped in detail")
	}
}

// =============================================================================
// mapToXML tests
// =============================================================================

func TestMapToXML_SimpleMap(t *testing.T) {
	m := map[string]interface{}{
		"name": "Alice",
		"age":  "30",
	}
	var buf bytes.Buffer
	mapToXML(&buf, m)
	s := buf.String()

	if !strings.Contains(s, "<name>Alice</name>") {
		t.Errorf("expected <name>Alice</name>, got: %s", s)
	}
	if !strings.Contains(s, "<age>30</age>") {
		t.Errorf("expected <age>30</age>, got: %s", s)
	}
}

func TestMapToXML_SkipsAttributesAndText(t *testing.T) {
	m := map[string]interface{}{
		"@id":   "42",
		"#text": "hello",
		"name":  "Alice",
	}
	var buf bytes.Buffer
	mapToXML(&buf, m)
	s := buf.String()

	if strings.Contains(s, "@id") {
		t.Error("should skip attribute keys")
	}
	if strings.Contains(s, "#text") {
		t.Error("should skip #text key")
	}
	if !strings.Contains(s, "<name>Alice</name>") {
		t.Error("expected name element")
	}
}

func TestMapToXML_FloatValue(t *testing.T) {
	m := map[string]interface{}{
		"price": float64(19.99),
	}
	var buf bytes.Buffer
	mapToXML(&buf, m)
	s := buf.String()

	if !strings.Contains(s, "<price>19.99</price>") {
		t.Errorf("expected <price>19.99</price>, got: %s", s)
	}
}

func TestMapToXML_BoolValue(t *testing.T) {
	m := map[string]interface{}{
		"active": true,
	}
	var buf bytes.Buffer
	mapToXML(&buf, m)
	s := buf.String()

	if !strings.Contains(s, "<active>true</active>") {
		t.Errorf("expected <active>true</active>, got: %s", s)
	}
}

func TestMapToXML_NilValue(t *testing.T) {
	m := map[string]interface{}{
		"empty": nil,
	}
	var buf bytes.Buffer
	mapToXML(&buf, m)
	s := buf.String()

	if !strings.Contains(s, "<empty/>") {
		t.Errorf("expected self-closing <empty/>, got: %s", s)
	}
}

func TestMapToXML_ArrayValue(t *testing.T) {
	m := map[string]interface{}{
		"item": []interface{}{"one", "two", "three"},
	}
	var buf bytes.Buffer
	mapToXML(&buf, m)
	s := buf.String()

	if strings.Count(s, "<item>") != 3 {
		t.Errorf("expected 3 <item> elements, got: %s", s)
	}
	if !strings.Contains(s, "<item>one</item>") {
		t.Error("missing item 'one'")
	}
}

// =============================================================================
// writeValue tests
// =============================================================================

func TestWriteValue_MapWithAttributes(t *testing.T) {
	val := map[string]interface{}{
		"@id":   "42",
		"@role": "admin",
		"name":  "Alice",
	}
	var buf bytes.Buffer
	writeValue(&buf, "user", val)
	s := buf.String()

	if !strings.HasPrefix(s, "<user") {
		t.Errorf("expected <user prefix, got: %s", s)
	}
	if !strings.Contains(s, `id="42"`) {
		t.Errorf("expected id attribute, got: %s", s)
	}
	if !strings.Contains(s, `role="admin"`) {
		t.Errorf("expected role attribute, got: %s", s)
	}
	if !strings.Contains(s, "<name>Alice</name>") {
		t.Errorf("expected name child, got: %s", s)
	}
	if !strings.HasSuffix(s, "</user>") {
		t.Errorf("expected </user> suffix, got: %s", s)
	}
}

func TestWriteValue_MapWithTextAndAttributes(t *testing.T) {
	val := map[string]interface{}{
		"@currency": "USD",
		"#text":     "19.99",
	}
	var buf bytes.Buffer
	writeValue(&buf, "price", val)
	s := buf.String()

	if !strings.Contains(s, `currency="USD"`) {
		t.Errorf("expected currency attribute, got: %s", s)
	}
	if !strings.Contains(s, "19.99") {
		t.Errorf("expected text content, got: %s", s)
	}
}

func TestWriteValue_XmlEscaping(t *testing.T) {
	var buf bytes.Buffer
	writeValue(&buf, "data", `<script>alert("xss")</script>`)
	s := buf.String()

	if strings.Contains(s, "<script>") {
		t.Error("should escape < in string values")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Error("expected escaped angle brackets")
	}
}

// =============================================================================
// getOperationConfig tests
// =============================================================================

// newTestFilter creates a soapRestFilter with the given config for testing.
// The Envoy SDK handle is nil, so only pure config/utility methods can be called.
func newTestFilter(ops map[string]*operationConfig, defaults defaultsConfig) *soapRestFilter {
	cfg := &filterConfig{
		Operations: ops,
		Defaults:   defaults,
	}
	if cfg.Operations == nil {
		cfg.Operations = make(map[string]*operationConfig)
	}
	cfg.precompute()
	return &soapRestFilter{
		config: cfg,
	}
}

func TestGetOperationConfig_FullyConfigured(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {
			RestMethod:   "GET",
			RestPath:     "/users/{id}",
			SoapEndpoint: "/ws/users",
		},
	}, defaultsConfig{})

	cfg := f.getOperationConfig("GetUser")
	if cfg.RestMethod != "GET" {
		t.Errorf("expected GET, got '%s'", cfg.RestMethod)
	}
	if cfg.RestPath != "/users/{id}" {
		t.Errorf("expected '/users/{id}', got '%s'", cfg.RestPath)
	}
	if cfg.SoapEndpoint != "/ws/users" {
		t.Errorf("expected '/ws/users', got '%s'", cfg.SoapEndpoint)
	}
}

func TestGetOperationConfig_PartialWithDefaults(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {
			RestMethod: "GET",
			RestPath:   "/users",
		},
	}, defaultsConfig{
		RestMethod:     "POST",
		RestPathPrefix: "/api",
		SoapEndpoint:   "/ws/default",
	})

	cfg := f.getOperationConfig("GetUser")
	if cfg.RestMethod != "GET" {
		t.Errorf("expected GET, got '%s'", cfg.RestMethod)
	}
	if cfg.RestPath != "/users" {
		t.Errorf("expected '/users', got '%s'", cfg.RestPath)
	}
	if cfg.SoapEndpoint != "/ws/default" {
		t.Errorf("expected '/ws/default', got '%s'", cfg.SoapEndpoint)
	}
}

func TestGetOperationConfig_Unknown(t *testing.T) {
	f := newTestFilter(nil, defaultsConfig{
		RestMethod:     "POST",
		RestPathPrefix: "/api",
		SoapEndpoint:   "/ws",
	})

	cfg := f.getOperationConfig("UnknownOp")
	if cfg.RestMethod != "POST" {
		t.Errorf("expected POST, got '%s'", cfg.RestMethod)
	}
	if cfg.RestPath != "/api/unknownop" {
		t.Errorf("expected '/api/unknownop', got '%s'", cfg.RestPath)
	}
}

func TestGetOperationConfig_UnknownWithEmptyDefaults(t *testing.T) {
	f := newTestFilter(nil, defaultsConfig{})

	cfg := f.getOperationConfig("Test")
	if cfg.RestMethod != "POST" {
		t.Errorf("expected default POST, got '%s'", cfg.RestMethod)
	}
	if cfg.RestPath != "/api/test" {
		t.Errorf("expected default '/api/test', got '%s'", cfg.RestPath)
	}
}

func TestGetOperationConfig_FillsMissingRestPath(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"CreateUser": {
			RestMethod: "POST",
		},
	}, defaultsConfig{
		RestPathPrefix: "/v2",
	})

	cfg := f.getOperationConfig("CreateUser")
	if cfg.RestPath != "/v2/createuser" {
		t.Errorf("expected '/v2/createuser', got '%s'", cfg.RestPath)
	}
}

// =============================================================================
// findOperationByRestPath tests
// =============================================================================

func TestFindOperationByRestPath_ExactMatch(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {RestPath: "/users"},
		"GetOrder": {RestPath: "/orders"},
	}, defaultsConfig{})

	result := f.findOperationByRestPath("/users")
	if result != "GetUser" {
		t.Errorf("expected 'GetUser', got '%s'", result)
	}
}

func TestFindOperationByRestPath_WithWildcard(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {RestPath: "/users/{id}"},
	}, defaultsConfig{})

	result := f.findOperationByRestPath("/users/42")
	if result != "GetUser" {
		t.Errorf("expected 'GetUser', got '%s'", result)
	}
}

func TestFindOperationByRestPath_NoMatch(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {RestPath: "/users"},
	}, defaultsConfig{})

	result := f.findOperationByRestPath("/orders")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestFindOperationByRestPath_DifferentSegmentCount(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {RestPath: "/users/{id}"},
	}, defaultsConfig{})

	result := f.findOperationByRestPath("/users/42/orders")
	if result != "" {
		t.Errorf("expected no match for path with extra segments, got '%s'", result)
	}
}

// =============================================================================
// findOperationBySoapAction tests
// =============================================================================

func TestFindOperationBySoapAction_Match(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {SoapAction: "http://example.com/GetUser"},
	}, defaultsConfig{})

	result := f.findOperationBySoapAction(`"http://example.com/GetUser"`)
	if result != "GetUser" {
		t.Errorf("expected 'GetUser', got '%s'", result)
	}
}

func TestFindOperationBySoapAction_NoMatch(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"GetUser": {SoapAction: "http://example.com/GetUser"},
	}, defaultsConfig{})

	result := f.findOperationBySoapAction("http://example.com/DeleteUser")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestFindOperationBySoapAction_WithQuotes(t *testing.T) {
	f := newTestFilter(map[string]*operationConfig{
		"CreateOrder": {SoapAction: "urn:CreateOrder"},
	}, defaultsConfig{})

	result := f.findOperationBySoapAction(`"urn:CreateOrder"`)
	if result != "CreateOrder" {
		t.Errorf("expected 'CreateOrder', got '%s'", result)
	}
}

// =============================================================================
// matchSegments tests
// =============================================================================

func TestMatchSegments_ExactMatch(t *testing.T) {
	template := []string{"users", "profile"}
	actual := []string{"users", "profile"}
	if !matchSegments(template, actual) {
		t.Error("expected match")
	}
}

func TestMatchSegments_WildcardMatch(t *testing.T) {
	template := []string{"users", "{id}"}
	actual := []string{"users", "42"}
	if !matchSegments(template, actual) {
		t.Error("expected match with wildcard")
	}
}

func TestMatchSegments_MultipleWildcards(t *testing.T) {
	template := []string{"users", "{userId}", "orders", "{orderId}"}
	actual := []string{"users", "42", "orders", "99"}
	if !matchSegments(template, actual) {
		t.Error("expected match with multiple wildcards")
	}
}

func TestMatchSegments_DifferentLength(t *testing.T) {
	template := []string{"users", "{id}"}
	actual := []string{"users"}
	if matchSegments(template, actual) {
		t.Error("expected no match for different lengths")
	}
}

func TestMatchSegments_Mismatch(t *testing.T) {
	template := []string{"users", "profile"}
	actual := []string{"users", "settings"}
	if matchSegments(template, actual) {
		t.Error("expected no match")
	}
}

func TestMatchSegments_EmptyTemplate(t *testing.T) {
	if !matchSegments([]string{}, []string{}) {
		t.Error("empty vs empty should match")
	}
}

// =============================================================================
// capitalize tests
// =============================================================================

func TestCapitalize_Normal(t *testing.T) {
	if capitalize("hello") != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", capitalize("hello"))
	}
}

func TestCapitalize_AlreadyCapitalized(t *testing.T) {
	if capitalize("Hello") != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", capitalize("Hello"))
	}
}

func TestCapitalize_SingleChar(t *testing.T) {
	if capitalize("a") != "A" {
		t.Errorf("expected 'A', got '%s'", capitalize("a"))
	}
}

func TestCapitalize_Empty(t *testing.T) {
	if capitalize("") != "" {
		t.Errorf("expected empty string, got '%s'", capitalize(""))
	}
}

func TestCapitalize_Unicode(t *testing.T) {
	result := capitalize("über")
	if result[0] < 'A' || result[0] > 'Z' {
		// ToUpper only works correctly on ASCII; the important thing is no panic
		t.Logf("unicode capitalize result: %s (may not fully uppercase non-ASCII)", result)
	}
}

// =============================================================================
// precompute tests
// =============================================================================

func TestPrecompute_SplitsPathSegments(t *testing.T) {
	cfg := &filterConfig{
		Operations: map[string]*operationConfig{
			"GetUser":    {RestPath: "/users/{id}"},
			"ListOrders": {RestPath: "/orders"},
			"Empty":      {RestPath: ""},
		},
	}
	cfg.precompute()

	op1 := cfg.Operations["GetUser"]
	if len(op1.pathSegments) != 2 || op1.pathSegments[0] != "users" || op1.pathSegments[1] != "{id}" {
		t.Errorf("expected [users {id}], got %v", op1.pathSegments)
	}

	op2 := cfg.Operations["ListOrders"]
	if len(op2.pathSegments) != 1 || op2.pathSegments[0] != "orders" {
		t.Errorf("expected [orders], got %v", op2.pathSegments)
	}

	op3 := cfg.Operations["Empty"]
	if len(op3.pathSegments) != 0 {
		t.Errorf("expected empty segments for empty path, got %v", op3.pathSegments)
	}
}

// =============================================================================
// filterConfig JSON unmarshalling tests
// =============================================================================

func TestFilterConfig_Unmarshal(t *testing.T) {
	jsonStr := `{
		"operations": {
			"GetUser": {
				"restMethod": "GET",
				"restPath": "/users/{id}",
				"soapAction": "http://example.com/GetUser",
				"soapEndpoint": "/ws/users"
			}
		},
		"defaults": {
			"restMethod": "POST",
			"restPathPrefix": "/api",
			"soapEndpoint": "/ws",
			"soapNamespace": "http://example.com/services"
		}
	}`

	var cfg filterConfig
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(cfg.Operations) != 1 {
		t.Errorf("expected 1 operation, got %d", len(cfg.Operations))
	}
	op := cfg.Operations["GetUser"]
	if op == nil {
		t.Fatal("expected GetUser operation")
	}
	if op.RestMethod != "GET" {
		t.Errorf("expected GET, got '%s'", op.RestMethod)
	}
	if op.RestPath != "/users/{id}" {
		t.Errorf("expected '/users/{id}', got '%s'", op.RestPath)
	}
	if cfg.Defaults.RestMethod != "POST" {
		t.Errorf("expected default POST, got '%s'", cfg.Defaults.RestMethod)
	}
	if cfg.Defaults.SoapNamespace != "http://example.com/services" {
		t.Errorf("expected namespace, got '%s'", cfg.Defaults.SoapNamespace)
	}
}

func TestFilterConfig_UnmarshalEmpty(t *testing.T) {
	var cfg filterConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if cfg.Operations != nil && len(cfg.Operations) != 0 {
		t.Errorf("expected nil or empty operations, got %v", cfg.Operations)
	}
}

// =============================================================================
// Roundtrip tests: XML → JSON → XML
// =============================================================================

func TestRoundtrip_SimpleSOAPToJSONAndBack(t *testing.T) {
	// Parse a SOAP request
	soapInput := `<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">
  <Body>
    <CreateUser><name>Alice</name><email>alice@example.com</email></CreateUser>
  </Body>
</Envelope>`

	env, err := parseSoapEnvelope([]byte(soapInput))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	opName, params, err := extractOperation(env.Body.Content)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	if opName != "CreateUser" {
		t.Errorf("expected 'CreateUser', got '%s'", opName)
	}

	// Convert to JSON
	jsonBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify JSON has the right fields
	var parsed map[string]interface{}
	json.Unmarshal(jsonBytes, &parsed)
	if parsed["name"] != "Alice" {
		t.Errorf("expected name=Alice in JSON, got %v", parsed["name"])
	}

	// Build back to SOAP
	soapOut := buildSoapEnvelope(opName+"Response", "http://example.com", params)
	sOut := string(soapOut)

	if !strings.Contains(sOut, "<ns:CreateUserResponse>") {
		t.Error("expected CreateUserResponse in rebuilt SOAP")
	}
	if !strings.Contains(sOut, "<name>Alice</name>") {
		t.Error("expected name=Alice in rebuilt SOAP")
	}
}

func TestRoundtrip_NestedSOAP(t *testing.T) {
	soapInput := `<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">
  <Body>
    <CreateOrder>
      <customer><name>Bob</name></customer>
      <item>Widget</item>
      <item>Gadget</item>
      <quantity>5</quantity>
    </CreateOrder>
  </Body>
</Envelope>`

	env, err := parseSoapEnvelope([]byte(soapInput))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	_, params, err := extractOperation(env.Body.Content)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	// Verify nested structure
	customer, ok := params["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for customer, got %T", params["customer"])
	}
	if customer["name"] != "Bob" {
		t.Errorf("expected customer name=Bob, got %v", customer["name"])
	}

	// Verify repeated elements become array
	items, ok := params["item"].([]interface{})
	if !ok {
		t.Fatalf("expected array for items, got %T", params["item"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Convert to JSON and back to SOAP
	jsonBytes, _ := json.Marshal(params)
	var roundtripped map[string]interface{}
	json.Unmarshal(jsonBytes, &roundtripped)

	soapOut := buildSoapEnvelope("CreateOrderResponse", "", roundtripped)
	s := string(soapOut)

	if !strings.Contains(s, "<CreateOrderResponse>") {
		t.Error("expected CreateOrderResponse in output")
	}
	if !strings.Contains(s, "<customer>") {
		t.Error("expected customer element in output")
	}
}

// =============================================================================
// sendJSONError JSON escaping tests
//
// These tests verify the safety fix applied to sendJSONError: error messages
// containing quotes, backslashes, or control characters must be properly escaped
// via json.Marshal to prevent producing invalid JSON responses.
// =============================================================================

func TestSendJSONError_EscapesQuotes(t *testing.T) {
	// Test the JSON escaping logic directly since we can't call sendJSONError
	// without the Envoy handle. Verify the escaping that json.Marshal does.
	detail := `error with "quotes" and \backslash`
	escaped, _ := json.Marshal(detail)

	// Should produce valid JSON string
	var result string
	if err := json.Unmarshal(escaped, &result); err != nil {
		t.Fatalf("json.Marshal produced invalid JSON: %v", err)
	}
	if result != detail {
		t.Errorf("roundtrip failed: got '%s'", result)
	}
}

func TestSendJSONError_EscapesControlChars(t *testing.T) {
	detail := "line1\nline2\ttab"
	escaped, _ := json.Marshal(detail)

	var result string
	if err := json.Unmarshal(escaped, &result); err != nil {
		t.Fatalf("json.Marshal produced invalid JSON: %v", err)
	}
	if result != detail {
		t.Errorf("roundtrip failed: got '%s'", result)
	}
}

// Verify the full JSON error structure is valid JSON
func TestSendJSONError_ProducesValidJSON(t *testing.T) {
	errorMsg := `bad "request"`
	detail := `field "name" has invalid value: <script>alert('xss')</script>`

	escapedMsg, _ := json.Marshal(errorMsg)
	escapedDetail, _ := json.Marshal(detail)

	var buf bytes.Buffer
	buf.WriteString(`{"error":`)
	buf.Write(escapedMsg)
	buf.WriteString(`,"detail":`)
	buf.Write(escapedDetail)
	buf.WriteByte('}')

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("produced invalid JSON: %v\nJSON: %s", err, buf.String())
	}
	if parsed["error"] != errorMsg {
		t.Errorf("error mismatch: got '%v'", parsed["error"])
	}
	if parsed["detail"] != detail {
		t.Errorf("detail mismatch: got '%v'", parsed["detail"])
	}
}

// =============================================================================
// Edge case and stress tests
// =============================================================================

func TestXmlToMap_DeeplyNested(t *testing.T) {
	data := []byte(`<a><b><c><d><e>deep</e></d></c></b></a>`)
	m := xmlToMap(data)
	a := m["a"].(map[string]interface{})
	b := a["b"].(map[string]interface{})
	c := b["c"].(map[string]interface{})
	d := c["d"].(map[string]interface{})
	if d["e"] != "deep" {
		t.Errorf("expected 'deep' at depth 5, got '%v'", d["e"])
	}
}

func TestXmlToMap_SelfClosingElement(t *testing.T) {
	data := []byte(`<name>Alice</name><avatar/>`)
	m := xmlToMap(data)
	if m["name"] != "Alice" {
		t.Errorf("expected 'Alice', got '%v'", m["name"])
	}
	// Self-closing element should have empty string value
	if m["avatar"] != "" {
		t.Errorf("expected empty string for self-closing element, got '%v'", m["avatar"])
	}
}

func TestBuildSoapEnvelope_LargeParams(t *testing.T) {
	params := make(map[string]interface{})
	for i := 0; i < 100; i++ {
		key := "field_" + strings.Repeat("x", 10)
		params[key] = strings.Repeat("value_", 20)
	}

	result := buildSoapEnvelope("BigOp", "http://example.com", params)
	if len(result) == 0 {
		t.Error("expected non-empty result for large params")
	}
	s := string(result)
	if !strings.HasPrefix(s, `<?xml version="1.0"`) {
		t.Error("missing XML declaration")
	}
	if !strings.HasSuffix(s, `</soap:Envelope>`) {
		t.Error("missing closing envelope")
	}
}

func TestMatchSegments_SingleSegment(t *testing.T) {
	if !matchSegments([]string{"users"}, []string{"users"}) {
		t.Error("single segment exact match should work")
	}
	if matchSegments([]string{"users"}, []string{"orders"}) {
		t.Error("single segment mismatch should fail")
	}
}

func TestMatchSegments_WildcardOnly(t *testing.T) {
	if !matchSegments([]string{"{anything}"}, []string{"foobar"}) {
		t.Error("single wildcard should match any value")
	}
}

func TestXmlToMap_WhitespaceOnly(t *testing.T) {
	data := []byte(`<name>   </name>`)
	m := xmlToMap(data)
	// TrimSpace should reduce to empty string
	if m["name"] != "" {
		t.Errorf("expected empty string after trimming whitespace, got '%v'", m["name"])
	}
}

func TestParseSoapEnvelope_ExtraWhitespace(t *testing.T) {
	input := `
	<?xml version="1.0" encoding="UTF-8"?>
	<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
		<soap:Body>
			<Ping/>
		</soap:Body>
	</soap:Envelope>
	`
	env, err := parseSoapEnvelope([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error with extra whitespace: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil envelope")
	}
}

// =============================================================================
// Benchmark tests
//
// Measure per-operation latency and allocations for the hot-path functions.
// Run with: go test -bench=. -benchmem ./...
// =============================================================================

func BenchmarkParseSoapEnvelope(b *testing.B) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser><id>42</id><name>Alice</name><email>alice@example.com</email></GetUser>
  </soap:Body>
</soap:Envelope>`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseSoapEnvelope(input)
	}
}

func BenchmarkExtractOperation(b *testing.B) {
	bodyContent := []byte(`<GetUser><id>42</id><name>Alice</name><email>alice@example.com</email></GetUser>`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractOperation(bodyContent)
	}
}

func BenchmarkXmlToMap(b *testing.B) {
	data := []byte(`<name>Alice</name><age>30</age><email>alice@example.com</email><city>NYC</city><country>US</country>`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		xmlToMap(data)
	}
}

func BenchmarkBuildSoapEnvelope(b *testing.B) {
	params := map[string]interface{}{
		"id":    "42",
		"name":  "Alice",
		"email": "alice@example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildSoapEnvelope("GetUserResponse", "http://example.com/services", params)
	}
}

func BenchmarkMapToXML(b *testing.B) {
	params := map[string]interface{}{
		"id":    "42",
		"name":  "Alice",
		"email": "alice@example.com",
		"address": map[string]interface{}{
			"city":    "NYC",
			"country": "US",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		mapToXML(&buf, params)
	}
}

func BenchmarkMatchSegments(b *testing.B) {
	template := []string{"users", "{id}", "orders", "{orderId}"}
	actual := []string{"users", "42", "orders", "99"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matchSegments(template, actual)
	}
}
