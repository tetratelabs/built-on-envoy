# SOAP-REST Bridge Extension

A bidirectional protocol bridge that transparently converts between SOAP XML and REST JSON at the Envoy proxy layer. Built as a Go Composer plugin using the Envoy Dynamic Modules SDK.

## Overview

This extension operates in two modes, detected automatically from request headers:

| Mode | Trigger | What It Does |
|------|---------|--------------|
| **SOAP to REST** | `Content-Type: text/xml` or `application/soap+xml` | Parses SOAP envelope, extracts operation + params, rewrites as REST JSON request. Wraps REST response back into SOAP envelope. |
| **REST to SOAP** | `Content-Type: application/json` + `X-Target-SOAPAction` header or config-matched path | Wraps JSON body in SOAP envelope, rewrites to SOAP endpoint. Unwraps SOAP response to JSON. |
| **Passthrough** | Anything else | No transformation. Request flows through unmodified. |

## Directory Structure

```
soap-rest/
├── plugin.go          # Main implementation (~1060 lines)
├── plugin_test.go     # Unit tests (75 tests + 6 benchmarks)
├── manifest.yaml      # Extension metadata
├── Makefile           # Build targets: build, install, clean
├── go.mod / go.sum    # Go module dependencies
├── buildandrun.sh     # Build + install + run with test config
├── test.sh            # Integration tests (18 test cases, 50 assertions)
└── rununitperf.sh     # Unit tests + benchmark runner
```

## Quick Start

### Build and Run

```bash
# Build the plugin
make build

# Install to boe data directory
make install

# Run with default config
boe run --extension soap-rest

# Or use the all-in-one script with test configuration
bash buildandrun.sh
```

### Send a SOAP Request

```bash
curl -X POST http://localhost:10000/ \
  -H "Content-Type: text/xml; charset=utf-8" \
  -H "SOAPAction: GetUser" \
  -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser>
      <UserId>42</UserId>
      <Name>Alice</Name>
    </GetUser>
  </soap:Body>
</soap:Envelope>'
```

The extension will:
1. Parse the SOAP envelope
2. Extract operation `GetUser` with params `{"UserId": "42", "Name": "Alice"}`
3. Rewrite as `GET /get` (or configured path) with JSON body
4. Send to upstream, get JSON response
5. Wrap response back in SOAP envelope and return to client

### Send a REST-to-SOAP Request

```bash
curl -X POST http://localhost:10000/users \
  -H "Content-Type: application/json" \
  -H "X-Target-SOAPAction: http://example.com/services/CreateUser" \
  -d '{"name": "Alice", "email": "alice@example.com"}'
```

The extension will:
1. Detect REST-to-SOAP mode via the `X-Target-SOAPAction` header
2. Wrap JSON params in a SOAP envelope with operation `CreateUser`
3. Forward to SOAP endpoint as `POST /ws` (or configured endpoint)
4. Unwrap SOAP response back to JSON and return to client

## Configuration

Pass configuration as JSON via `--config`:

```bash
boe run --extension soap-rest --config '{
  "operations": {
    "GetUser": {
      "restMethod": "GET",
      "restPath": "/users/{id}",
      "pathParams": {"id": "UserId"},
      "soapAction": "http://example.com/GetUser",
      "soapEndpoint": "/ws/users"
    },
    "CreateOrder": {
      "restMethod": "POST",
      "restPath": "/orders"
    }
  },
  "defaults": {
    "restMethod": "POST",
    "restPathPrefix": "/api",
    "soapEndpoint": "/ws",
    "soapNamespace": "http://example.com/services"
  }
}'
```

### Configuration Reference

#### `operations` (map of operation configs)

Each key is a SOAP operation name. Values:

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `restMethod` | string | HTTP method for REST side | `"GET"`, `"POST"` |
| `restPath` | string | REST URL path (supports `{param}` placeholders) | `"/users/{id}"` |
| `pathParams` | map | Maps path placeholder names to SOAP XML element names | `{"id": "UserId"}` |
| `soapAction` | string | SOAPAction header value for REST-to-SOAP | `"urn:GetUser"` |
| `soapEndpoint` | string | SOAP service endpoint path | `"/ws/users"` |

#### `defaults`

Fallback values when an operation isn't explicitly configured:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `restMethod` | string | `"POST"` | Default HTTP method |
| `restPathPrefix` | string | `"/api"` | Prefix for auto-generated REST paths |
| `soapEndpoint` | string | `"/ws"` | Default SOAP service endpoint |
| `soapNamespace` | string | `""` | XML namespace URI for SOAP envelopes |

When an unknown operation is received, it maps to `POST /api/{operationname}` by default.

## Architecture

### Mode Detection (`detectMode`)

```
Request arrives
    |
    +-- Content-Type contains "text/xml" or "application/soap+xml"?
    |     YES --> modeSoapToRest
    |
    +-- Content-Type contains "application/json"?
    |     +-- Has X-Target-SOAPAction header? --> modeRestToSoap
    |     +-- Path matches a configured operation? --> modeRestToSoap
    |
    +-- Otherwise --> modePassthrough
```

### Request/Response Flow

```
SOAP-to-REST:
  Client  -->  [SOAP XML]  -->  Filter  -->  [REST JSON]  -->  Upstream
  Client  <--  [SOAP XML]  <--  Filter  <--  [REST JSON]  <--  Upstream

REST-to-SOAP:
  Client  -->  [REST JSON]  -->  Filter  -->  [SOAP XML]  -->  Upstream
  Client  <--  [REST JSON]  <--  Filter  <--  [SOAP XML]  <--  Upstream
```

### Key Components

| Component | Function |
|-----------|----------|
| `parseSoapEnvelope` | Parses SOAP 1.1/1.2 XML into structured envelope |
| `extractOperation` | Extracts operation name and params from `<soap:Body>` |
| `xmlToMap` / `populateMap` / `decodeElement` | Generic XML-to-map conversion with attribute and array support |
| `buildSoapEnvelope` | Builds SOAP XML from operation name, namespace, and param map |
| `buildSoapFault` | Builds SOAP Fault envelope from HTTP status and detail |
| `mapToXML` / `writeValue` | Recursive map-to-XML conversion with type handling |
| `detectMode` | Determines SOAP-to-REST, REST-to-SOAP, or passthrough |
| `getOperationConfig` | Resolves operation config with fallback to defaults |
| `matchSegments` | Path template matching using pre-computed segments |

### XML-JSON Mapping Rules

| XML Construct | JSON Representation |
|---------------|-------------------|
| `<name>Alice</name>` | `{"name": "Alice"}` |
| `<item>A</item><item>B</item>` | `{"item": ["A", "B"]}` (repeated elements become arrays) |
| `<user><name>Bob</name></user>` | `{"user": {"name": "Bob"}}` (nested elements become nested objects) |
| `<price currency="USD">19.99</price>` | `{"price": {"@currency": "USD", "#text": "19.99"}}` (attributes get `@` prefix) |
| `<empty/>` | `{"empty": ""}` |

### SOAP Fault Handling

- **SOAP-to-REST**: If the upstream returns an HTTP error, the response is wrapped as a SOAP Fault with `<faultcode>soap:Server</faultcode>` and the HTTP status in `<faultstring>`.
- **REST-to-SOAP**: If the SOAP response contains a `<Fault>`, it is converted to an HTTP 500 JSON response with `{"error": "...", "faultCode": "...", "detail": "..."}`.

## Testing

### Unit Tests

```bash
# Run all unit tests
go test -v ./...

# Run with benchmarks
bash rununitperf.sh

# Run only unit tests
bash rununitperf.sh --unit

# Run only benchmarks
bash rununitperf.sh --bench

# Run with coverage report
bash rununitperf.sh --cover
```

**75 unit tests** covering:
- SOAP envelope parsing (valid, invalid, faults, headers, SOAP 1.1/1.2)
- XML-to-map conversion (simple, nested, attributes, arrays, edge cases)
- SOAP envelope building (namespaces, special characters, nil params)
- Configuration helpers (operation lookup, path matching, defaults)
- JSON escaping safety
- Roundtrip conversions (XML to JSON to XML)

**6 benchmarks** for hot-path functions.

### Integration Tests

Requires the extension running with `buildandrun.sh`:

```bash
# Terminal 1: Start the extension
bash buildandrun.sh

# Terminal 2: Run integration tests
bash test.sh
```

**18 test cases** (50 assertions) covering:
- Basic SOAP-to-REST conversion
- Configured operation mappings
- Nested XML structures
- SOAP 1.2 envelopes
- XML attributes
- Malformed SOAP (error handling)
- REST-to-SOAP via header
- REST-to-SOAP via config path
- Empty body handling
- Passthrough for non-SOAP/REST traffic
- Namespace prefixes
- SOAP Header elements
- Special XML characters
- Large payloads

Options: `bash test.sh -v` for verbose output, `bash test.sh -t 5` to run a specific test.

## Performance

Benchmark results on Apple M3 Pro (from `rununitperf.sh --bench`):

| Function | ns/op | B/op | allocs/op |
|----------|-------|------|-----------|
| ParseSoapEnvelope | ~10,000 | 4,736 | 104 |
| ExtractOperation | ~5,300 | 3,560 | 75 |
| XmlToMap | ~3,600 | 2,128 | 52 |
| BuildSoapEnvelope | ~340 | 496 | 2 |
| MapToXML | ~500 | 240 | 3 |
| MatchSegments | ~11 | 0 | 0 |

### Optimizations Applied

- Singleton `strings.Replacer` for XML escaping (avoids per-call allocation)
- `bytes.Buffer` with pre-sized `Grow()` instead of `fmt.Sprintf`
- Pre-computed path template segments at config load time
- Cached operation lookup between mode detection and transformation
- Lazy map allocation in XML decoder (only when children exist)
- `strings.LastIndexByte` instead of `strings.Split` for single-character delimiters
- `strconv.FormatFloat`/`FormatBool` instead of `fmt.Sprintf` for numeric types

## License

Apache-2.0
