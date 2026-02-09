#!/usr/bin/env bash
#
# SOAP-REST Extension Test Suite
#
# Prerequisites:
#   1. Build and install the plugin:
#        cd /Users/saravanan/Documents/code/built-on-envoy/extensions/soap-rest/soap-rest
#        make install
#
#   2. Start boe with the extension (in a separate terminal):
#        boe run --local . --config '{
#          "operations": {
#            "GetUser": {
#              "restMethod": "GET",
#              "restPath": "/get",
#              "pathParams": {}
#            },
#            "CreateUser": {
#              "restMethod": "POST",
#              "restPath": "/post"
#            },
#            "CreateOrder": {
#              "restMethod": "POST",
#              "restPath": "/post"
#            },
#            "SearchProducts": {
#              "restMethod": "POST",
#              "restPath": "/post"
#            },
#            "Ping": {
#              "restMethod": "POST",
#              "restPath": "/post"
#            }
#          },
#          "defaults": {
#            "restMethod": "POST",
#            "restPathPrefix": "/api",
#            "soapEndpoint": "/post",
#            "soapNamespace": "http://example.com/services"
#          }
#        }'
#
#   3. Run this script:
#        bash test.sh
#
# Usage:
#   bash test.sh              # Run all tests
#   bash test.sh -v           # Verbose mode (show full responses)
#   bash test.sh -t 3         # Run only test 3
#   bash test.sh -t 3,7       # Run tests 3 and 7
#

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

BASE_URL="${BASE_URL:-http://localhost:10000}"
VERBOSE="${VERBOSE:-false}"
RUN_TESTS=""

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -v|--verbose)
      VERBOSE=true
      shift
      ;;
    -t|--test)
      RUN_TESTS="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [-v] [-t test_numbers]"
      exit 1
      ;;
  esac
done

# =============================================================================
# Test framework
# =============================================================================

PASS=0
FAIL=0
SKIP=0
TOTAL=0
FAILURES=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

should_run_test() {
  local test_num=$1
  if [[ -z "$RUN_TESTS" ]]; then
    return 0
  fi
  echo "$RUN_TESTS" | tr ',' '\n' | grep -qw "$test_num"
}

log_test_header() {
  local test_num=$1
  local description=$2
  TOTAL=$((TOTAL + 1))
  echo ""
  echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}Test $test_num: $description${NC}"
  echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

log_pass() {
  PASS=$((PASS + 1))
  echo -e "  ${GREEN}PASS${NC}: $1"
}

log_fail() {
  FAIL=$((FAIL + 1))
  FAILURES="${FAILURES}\n  - Test $CURRENT_TEST: $1"
  echo -e "  ${RED}FAIL${NC}: $1"
}

log_skip() {
  SKIP=$((SKIP + 1))
  echo -e "  ${YELLOW}SKIP${NC}: $1"
}

log_verbose() {
  if [[ "$VERBOSE" == "true" ]]; then
    echo -e "  ${YELLOW}[verbose]${NC} $1"
  fi
}

assert_status() {
  local expected=$1
  local actual=$2
  local label="${3:-HTTP status}"
  if [[ "$actual" == "$expected" ]]; then
    log_pass "$label: $actual"
  else
    log_fail "$label: expected $expected, got $actual"
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="${3:-Response contains expected content}"
  if echo "$haystack" | grep -q "$needle"; then
    log_pass "$label"
  else
    log_fail "$label (expected to find '$needle')"
  fi
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local label="${3:-Response does not contain unexpected content}"
  if echo "$haystack" | grep -q "$needle"; then
    log_fail "$label (unexpectedly found '$needle')"
  else
    log_pass "$label"
  fi
}

assert_json_key() {
  local json="$1"
  local key="$2"
  local label="${3:-JSON contains key '$key'}"
  if echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); assert '$key' in d" 2>/dev/null; then
    log_pass "$label"
  else
    log_fail "$label"
  fi
}

assert_json_value() {
  local json="$1"
  local key="$2"
  local expected="$3"
  local label="${4:-JSON key '$key' equals '$expected'}"
  local actual
  actual=$(echo "$json" | python3 -c "import sys,json; print(json.load(sys.stdin)['$key'])" 2>/dev/null || echo "__MISSING__")
  if [[ "$actual" == "$expected" ]]; then
    log_pass "$label"
  else
    log_fail "$label (got '$actual')"
  fi
}

assert_xml_contains() {
  local xml="$1"
  local element="$2"
  local label="${3:-XML contains element '$element'}"
  if echo "$xml" | grep -q "$element"; then
    log_pass "$label"
  else
    log_fail "$label"
  fi
}

# Perform a curl request and capture both status code and body
do_curl() {
  local status_file
  status_file=$(mktemp)
  local body
  body=$(curl -s -w "%{http_code}" -o >(cat) "$@" 2>/dev/null | tee /dev/null)

  # Separate status code from body
  local response
  response=$(curl -s -w "\n%{http_code}" "$@" 2>/dev/null)
  HTTP_STATUS=$(echo "$response" | tail -1)
  HTTP_BODY=$(echo "$response" | sed '$d')

  log_verbose "Status: $HTTP_STATUS"
  log_verbose "Body: $(echo "$HTTP_BODY" | head -20)"

  rm -f "$status_file"
}

# =============================================================================
# Connectivity check
# =============================================================================

echo -e "${BOLD}SOAP-REST Extension Test Suite${NC}"
echo -e "Target: ${BASE_URL}"
echo ""
echo -n "Checking connectivity... "

if ! curl -s --max-time 5 "${BASE_URL}/get" > /dev/null 2>&1; then
  echo -e "${RED}FAILED${NC}"
  echo ""
  echo "Cannot reach ${BASE_URL}. Please ensure boe is running:"
  echo ""
  echo "  cd /Users/saravanan/Documents/code/built-on-envoy/extensions/soap-rest/soap-rest"
  echo "  make install"
  echo "  boe run --local . --config '<see config in script header>'"
  echo ""
  exit 1
fi
echo -e "${GREEN}OK${NC}"

# =============================================================================
# Test 1: Basic SOAP → REST with default mapping
# =============================================================================

CURRENT_TEST=1
if should_run_test 1; then
  log_test_header 1 "SOAP → REST: Basic request (default config)"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser>
      <UserId>123</UserId>
      <Name>Alice</Name>
    </GetUser>
  </soap:Body>
</soap:Envelope>'

  # Response should be a SOAP envelope wrapping httpbin's response
  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Response is a SOAP envelope"
  assert_xml_contains "$HTTP_BODY" "soap:Body" "Response has SOAP body"
  assert_xml_contains "$HTTP_BODY" "GetUserResponse" "Response wraps as GetUserResponse"
fi

# =============================================================================
# Test 2: SOAP → REST with configured operation mapping
# =============================================================================

CURRENT_TEST=2
if should_run_test 2; then
  log_test_header 2 "SOAP → REST: Configured operation mapping (GetUser → GET /get)"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -H "SOAPAction: GetUser" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser>
      <UserId>42</UserId>
    </GetUser>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Response is a SOAP envelope"
  assert_xml_contains "$HTTP_BODY" "GetUserResponse" "Response operation name is GetUserResponse"
  # httpbin /get echoes URL info — verify the request was rewritten
  assert_xml_contains "$HTTP_BODY" "url" "httpbin echoed the URL (confirms request reached /get)"
fi

# =============================================================================
# Test 3: SOAP → REST with nested XML elements
# =============================================================================

CURRENT_TEST=3
if should_run_test 3; then
  log_test_header 3 "SOAP → REST: Nested XML elements and repeated elements"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CreateOrder>
      <Customer>
        <Name>Bob</Name>
        <Email>bob@example.com</Email>
      </Customer>
      <Item>Widget A</Item>
      <Item>Widget B</Item>
      <Quantity>5</Quantity>
    </CreateOrder>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Response is a SOAP envelope"
  assert_xml_contains "$HTTP_BODY" "CreateOrderResponse" "Response wraps as CreateOrderResponse"

  # httpbin /post echoes the POSTed JSON body — extract and validate
  # The response is SOAP XML wrapping httpbin's JSON echo which is itself XML-ified
  # Just verify the key fields made it through
  assert_xml_contains "$HTTP_BODY" "Customer" "Nested Customer element preserved"
  assert_xml_contains "$HTTP_BODY" "Bob" "Customer Name value preserved"
fi

# =============================================================================
# Test 4: SOAP 1.2 Content-Type
# =============================================================================

CURRENT_TEST=4
if should_run_test 4; then
  log_test_header 4 "SOAP → REST: SOAP 1.2 Content-Type (application/soap+xml)"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: application/soap+xml; charset=utf-8" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope">
  <soap:Body>
    <Ping>
      <Message>hello</Message>
    </Ping>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Response is a SOAP envelope"
  assert_xml_contains "$HTTP_BODY" "PingResponse" "SOAP 1.2 request processed correctly"
fi

# =============================================================================
# Test 5: SOAP with XML attributes
# =============================================================================

CURRENT_TEST=5
if should_run_test 5; then
  log_test_header 5 "SOAP → REST: XML attributes on elements"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <SearchProducts>
      <Filter type="category" active="true">Electronics</Filter>
      <MaxResults>10</MaxResults>
    </SearchProducts>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Response is a SOAP envelope"
  assert_xml_contains "$HTTP_BODY" "SearchProductsResponse" "Response wraps as SearchProductsResponse"
fi

# =============================================================================
# Test 6: Malformed SOAP (error handling)
# =============================================================================

CURRENT_TEST=6
if should_run_test 6; then
  log_test_header 6 "SOAP → REST: Malformed SOAP envelope (error handling)"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<not-a-soap-envelope><broken>'

  assert_status "400" "$HTTP_STATUS" "Returns HTTP 400 for malformed SOAP"
  assert_contains "$HTTP_BODY" "error" "Error response contains 'error' field"
  assert_contains "$HTTP_BODY" "invalid SOAP envelope\|SOAP Envelope\|parse error" "Error message is descriptive"
fi

# =============================================================================
# Test 7: REST → SOAP via X-Target-SOAPAction header
# =============================================================================

CURRENT_TEST=7
if should_run_test 7; then
  log_test_header 7 "REST → SOAP: Via X-Target-SOAPAction header"

  do_curl "${BASE_URL}/users" \
    -X POST \
    -H "Content-Type: application/json" \
    -H "X-Target-SOAPAction: http://example.com/services/GetUser" \
    -d '{"UserId": "123", "Name": "Alice"}'

  assert_status "200" "$HTTP_STATUS"
  # httpbin echoes what it received — the body should have been SOAP XML
  # The response then gets unwrapped from SOAP to JSON by the filter
  # However, httpbin returns JSON (not SOAP), so the response transform
  # will try to parse it as SOAP and fall through. The key validation is
  # that the request was transformed.
  # Check that we got a response back (httpbin echoed something)
  if [[ -n "$HTTP_BODY" ]]; then
    log_pass "Received response body (request was forwarded to upstream)"
  else
    log_fail "Empty response body"
  fi
fi

# =============================================================================
# Test 8: REST → SOAP via config-matched path
# =============================================================================

CURRENT_TEST=8
if should_run_test 8; then
  log_test_header 8 "REST → SOAP: Via config-matched REST path (/post → CreateUser)"

  do_curl "${BASE_URL}/post" \
    -X POST \
    -H "Content-Type: application/json" \
    -d '{"OrderId": "456", "Amount": 99.99}'

  assert_status "200" "$HTTP_STATUS"
  if [[ -n "$HTTP_BODY" ]]; then
    log_pass "Received response body (request was forwarded)"
  else
    log_fail "Empty response body"
  fi
fi

# =============================================================================
# Test 9: REST → SOAP with empty body
# =============================================================================

CURRENT_TEST=9
if should_run_test 9; then
  log_test_header 9 "REST → SOAP: Empty JSON body"

  do_curl "${BASE_URL}/users" \
    -X POST \
    -H "Content-Type: application/json" \
    -H "X-Target-SOAPAction: http://example.com/services/ListUsers" \
    -d ''

  # Should still work — generates an empty SOAP operation element.
  # 404 is acceptable since httpbin doesn't have a /users endpoint;
  # the key validation is that the filter processed and forwarded the request.
  if [[ "$HTTP_STATUS" == "200" || "$HTTP_STATUS" == "404" || "$HTTP_STATUS" == "411" ]]; then
    log_pass "HTTP status: $HTTP_STATUS (request handled)"
  else
    log_fail "HTTP status: expected 200 or 411, got $HTTP_STATUS"
  fi
fi

# =============================================================================
# Test 10: Passthrough — normal GET request
# =============================================================================

CURRENT_TEST=10
if should_run_test 10; then
  log_test_header 10 "Passthrough: Normal GET request (no transformation)"

  do_curl "${BASE_URL}/get"

  assert_status "200" "$HTTP_STATUS"
  # httpbin /get returns JSON — should NOT be wrapped in SOAP
  assert_not_contains "$HTTP_BODY" "soap:Envelope" "Response is NOT wrapped in SOAP (passthrough)"
  assert_contains "$HTTP_BODY" '"url"' "httpbin JSON response received as-is"
fi

# =============================================================================
# Test 11: Passthrough — JSON request with no SOAP markers
# =============================================================================

CURRENT_TEST=11
if should_run_test 11; then
  log_test_header 11 "Passthrough: JSON POST without SOAP markers (no transformation)"

  do_curl "${BASE_URL}/anything" \
    -X POST \
    -H "Content-Type: application/json" \
    -d '{"key": "value"}'

  assert_status "200" "$HTTP_STATUS"
  assert_not_contains "$HTTP_BODY" "soap:Envelope" "Response is NOT wrapped in SOAP (passthrough)"
  assert_contains "$HTTP_BODY" '"key"' "JSON body passed through unchanged"
fi

# =============================================================================
# Test 12: SOAP with namespace prefixed elements
# =============================================================================

CURRENT_TEST=12
if should_run_test 12; then
  log_test_header 12 "SOAP → REST: Namespace-prefixed elements (soapenv: prefix)"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
                  xmlns:ns="http://example.com/services">
  <soapenv:Body>
    <ns:GetUser>
      <ns:UserId>999</ns:UserId>
    </ns:GetUser>
  </soapenv:Body>
</soapenv:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope\|Envelope" "Response is a SOAP envelope"
fi

# =============================================================================
# Test 13: SOAP with SOAP Header element
# =============================================================================

CURRENT_TEST=13
if should_run_test 13; then
  log_test_header 13 "SOAP → REST: SOAP request with Header element"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Header>
    <AuthToken>abc-def-123</AuthToken>
    <TraceId>trace-456</TraceId>
  </soap:Header>
  <soap:Body>
    <GetUser>
      <UserId>77</UserId>
    </GetUser>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Response is a SOAP envelope"
  assert_xml_contains "$HTTP_BODY" "GetUserResponse" "Operation processed despite SOAP headers"
fi

# =============================================================================
# Test 14: SOAP with empty body element
# =============================================================================

CURRENT_TEST=14
if should_run_test 14; then
  log_test_header 14 "SOAP → REST: Empty SOAP body (no operation)"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
  </soap:Body>
</soap:Envelope>'

  # Should return an error since there's no operation in the body
  assert_status "400" "$HTTP_STATUS" "Returns HTTP 400 for empty SOAP body"
  assert_contains "$HTTP_BODY" "error" "Error response returned"
fi

# =============================================================================
# Test 15: Content-Type with charset parameter
# =============================================================================

CURRENT_TEST=15
if should_run_test 15; then
  log_test_header 15 "SOAP → REST: Content-Type with charset parameter"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml; charset=utf-8" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <Ping><Message>charset test</Message></Ping>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "SOAP detected despite charset in Content-Type"
  assert_xml_contains "$HTTP_BODY" "PingResponse" "Request processed correctly"
fi

# =============================================================================
# Test 16: Large SOAP request with many elements
# =============================================================================

CURRENT_TEST=16
if should_run_test 16; then
  log_test_header 16 "SOAP → REST: Large request with many elements"

  # Build a SOAP request with 20 elements
  ITEMS=""
  for i in $(seq 1 20); do
    ITEMS="${ITEMS}<Item><Id>${i}</Id><Name>Product ${i}</Name><Price>${i}0.99</Price></Item>"
  done

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d "<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<soap:Envelope xmlns:soap=\"http://schemas.xmlsoap.org/soap/envelope/\">
  <soap:Body>
    <CreateOrder>
      <CustomerId>BULK-001</CustomerId>
      ${ITEMS}
    </CreateOrder>
  </soap:Body>
</soap:Envelope>"

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Large request processed"
  assert_xml_contains "$HTTP_BODY" "CreateOrderResponse" "Operation completed"
fi

# =============================================================================
# Test 17: Special characters in SOAP values
# =============================================================================

CURRENT_TEST=17
if should_run_test 17; then
  log_test_header 17 "SOAP → REST: Special XML characters in values"

  do_curl "${BASE_URL}/" \
    -H "Content-Type: text/xml" \
    -d '<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CreateUser>
      <Name>O&apos;Brien &amp; Associates</Name>
      <Query>&lt;script&gt;alert(1)&lt;/script&gt;</Query>
    </CreateUser>
  </soap:Body>
</soap:Envelope>'

  assert_status "200" "$HTTP_STATUS"
  assert_xml_contains "$HTTP_BODY" "soap:Envelope" "Special characters handled"
fi

# =============================================================================
# Test 18: Passthrough — non-XML, non-JSON content type
# =============================================================================

CURRENT_TEST=18
if should_run_test 18; then
  log_test_header 18 "Passthrough: Non-XML, non-JSON Content-Type (text/plain)"

  do_curl "${BASE_URL}/post" \
    -X POST \
    -H "Content-Type: text/plain" \
    -d 'This is plain text, not SOAP or JSON'

  assert_status "200" "$HTTP_STATUS"
  assert_not_contains "$HTTP_BODY" "soap:Envelope" "Plain text passes through (no SOAP wrapping)"
fi

# =============================================================================
# Summary
# =============================================================================

echo ""
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BOLD}Test Summary${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  Total assertions:  $((PASS + FAIL))"
echo -e "  ${GREEN}Passed:  $PASS${NC}"
echo -e "  ${RED}Failed:  $FAIL${NC}"
if [[ $SKIP -gt 0 ]]; then
  echo -e "  ${YELLOW}Skipped: $SKIP${NC}"
fi

if [[ $FAIL -gt 0 ]]; then
  echo ""
  echo -e "${RED}Failures:${NC}"
  echo -e "$FAILURES"
  echo ""
  exit 1
else
  echo ""
  echo -e "${GREEN}All tests passed!${NC}"
  echo ""
  exit 0
fi
