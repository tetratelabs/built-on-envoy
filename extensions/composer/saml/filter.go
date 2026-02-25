// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"bytes"
	"errors"
	"net/url"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// metadataCalloutTimeoutMs is the timeout for the IdP metadata HttpCallout (5 seconds).
const metadataCalloutTimeoutMs = 5000

// samlHTTPFilter is the per-request HTTP filter implementation.
// It handles the full SAML SP flow: session validation, AuthnRequest generation,
// ACS endpoint processing, and SP metadata serving.
type samlHTTPFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	cfg    *samlFilterConfig

	// isACSRequest tracks whether the current request is an ACS POST.
	isACSRequest bool
	// requestScheme, requestHost, and requestID are captured in OnRequestHeaders for use in OnRequestBody.
	requestScheme string
	requestHost   string
	requestID     string
	// requestHeaders and headerEndStream are stored during OnRequestHeaders for use
	// after a lazy IdP metadata fetch completes. When metadata is fetched via HttpCallout,
	// OnRequestHeaders is not re-invoked, so the auth flow must run explicitly using
	// the stored headers.
	requestHeaders  shared.HeaderMap
	headerEndStream bool
}

// logger is an interface for logging.
// shared.HttpFilterHandle implements this interface.
type logger interface {
	Log(level shared.LogLevel, format string, args ...any)
}

// OnRequestHeaders intercepts incoming requests and applies SAML authentication logic.
//
// Flow:
//  1. Bypass paths → continue without auth.
//  2. Lazy metadata fetch → if metadata not yet loaded, fetch via HttpCallout.
//  3. SP metadata path → serve metadata XML.
//  4. ACS path (POST) → buffer body for processing in OnRequestBody.
//  5. Valid session cookie → set upstream headers and continue.
//  6. No/invalid session → redirect to IdP with AuthnRequest.
func (f *samlHTTPFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	path := getRequestPath(f.handle)
	method := getRequestMethod(f.handle)
	f.requestID = headers.GetOne("x-request-id")

	f.handle.Log(shared.LogLevelDebug, "saml: [%s] handling %s %s", f.requestID, method, path)

	cfg := f.cfg.config

	// 1. Check bypass paths.
	for _, bp := range cfg.BypassPaths {
		if path == bp {
			f.handle.Log(shared.LogLevelDebug, "saml: [%s] bypassing auth for path %s", f.requestID, path)
			return shared.HeadersStatusContinue
		}
	}

	// 2. Lazy metadata fetch: if metadata is not yet loaded (URL mode), fetch it.
	if f.cfg.idpMetadata == nil && f.cfg.idpMetadataURL != "" {
		// Store headers for use in the metadata fetch callback, where the auth flow
		// must run explicitly since OnRequestHeaders won't be re-invoked.
		f.requestHeaders = headers
		f.headerEndStream = endStream
		return f.fetchMetadataOrWait()
	}

	return f.processAuthenticatedRequest(headers, endStream)
}

// fetchMetadataOrWait initiates or waits for the IdP metadata fetch via HttpCallout.
// Returns HeadersStatusStopAllAndBuffer to pause the request until metadata is available.
func (f *samlHTTPFilter) fetchMetadataOrWait() shared.HeadersStatus {
	f.cfg.mu.Lock()

	// Double-check after acquiring the lock — metadata may have been loaded while we waited.
	if f.cfg.idpMetadata != nil {
		f.cfg.mu.Unlock()
		f.handle.Log(shared.LogLevelDebug, "saml: [%s] metadata already available after lock acquisition", f.requestID)
		// Metadata became available while waiting for the lock. Since we are still inside
		// OnRequestHeaders, we can proceed directly with the auth flow.
		return f.processAuthenticatedRequest(f.requestHeaders, f.headerEndStream)
	}

	if f.cfg.metadataFetching {
		// Another request already triggered the fetch — just wait.
		f.handle.Log(shared.LogLevelDebug, "saml: [%s] metadata fetch in progress, waiting", f.requestID)
		f.cfg.pendingRequests = append(f.cfg.pendingRequests, f)
		f.cfg.mu.Unlock()
		return shared.HeadersStatusStopAllAndBuffer
	}

	// We are the first — initiate the fetch.
	f.cfg.metadataFetching = true
	f.cfg.mu.Unlock()

	f.handle.Log(shared.LogLevelInfo, "saml: [%s] fetching idp metadata from %s", f.requestID, f.cfg.idpMetadataURL)

	calloutHeaders, err := buildMetadataCalloutHeaders(f.cfg.idpMetadataURL)
	if err != nil {
		f.cfg.mu.Lock()
		f.cfg.metadataFetching = false
		f.cfg.mu.Unlock()
		f.handle.Log(shared.LogLevelError, "saml: [%s] invalid idp_metadata_url: %s", f.requestID, err.Error())
		f.handle.SendLocalResponse(503, nil, []byte("Service Unavailable: invalid IdP metadata URL"), "saml")
		return shared.HeadersStatusStop
	}

	initResult, _ := f.handle.HttpCallout(
		f.cfg.idpMetadataCluster,
		calloutHeaders,
		nil,
		metadataCalloutTimeoutMs,
		f,
	)

	if initResult != shared.HttpCalloutInitSuccess {
		f.cfg.mu.Lock()
		f.cfg.metadataFetching = false
		f.cfg.mu.Unlock()
		f.handle.Log(shared.LogLevelError, "saml: [%s] failed to initiate metadata callout: %d", f.requestID, initResult)
		f.handle.SendLocalResponse(503, nil, []byte("Service Unavailable: failed to fetch IdP metadata"), "saml")
		return shared.HeadersStatusStop
	}

	return shared.HeadersStatusStopAllAndBuffer
}

// OnHttpCalloutDone is called when the IdP metadata HttpCallout completes.
// It implements shared.HttpCalloutCallback.
func (f *samlHTTPFilter) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]string, body [][]byte) { //nolint:revive // method name must match SDK interface
	f.cfg.mu.Lock()

	if result != shared.HttpCalloutSuccess {
		f.handle.Log(shared.LogLevelError, "saml: [%s] metadata callout failed with result: %d", f.requestID, result)
		f.failPendingRequests("Service Unavailable: IdP metadata fetch failed")
		return
	}

	// Check HTTP status from response headers.
	status := getCalloutResponseStatus(headers)
	if status != "200" {
		f.handle.Log(shared.LogLevelError, "saml: [%s] metadata callout returned HTTP status: %s", f.requestID, status)
		f.failPendingRequests("Service Unavailable: IdP metadata endpoint returned " + status)
		return
	}

	// Concatenate body chunks.
	metadataXML := concatBodyChunks(body)
	if len(metadataXML) == 0 {
		f.handle.Log(shared.LogLevelError, "saml: [%s] metadata callout returned empty body", f.requestID)
		f.failPendingRequests("Service Unavailable: IdP metadata response was empty")
		return
	}

	// Parse the metadata.
	idpMeta, err := parseIDPMetadata(metadataXML)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "saml: [%s] failed to parse fetched idp metadata: %s", f.requestID, err.Error())
		f.failPendingRequests("Service Unavailable: invalid IdP metadata")
		return
	}

	// Store the metadata and resume all pending requests.
	f.cfg.idpMetadata = idpMeta
	f.cfg.metadataFetching = false
	pending := f.cfg.pendingRequests
	f.cfg.pendingRequests = nil
	f.cfg.mu.Unlock()

	f.handle.Log(shared.LogLevelInfo, "saml: [%s] fetched idp metadata for entity_id=%s, sso_url=%s",
		f.requestID, idpMeta.EntityID, idpMeta.SSOURL)

	// Run the SAML auth flow for the originating request. OnRequestHeaders is not
	// re-invoked after ContinueRequest, so the auth logic must run explicitly here.
	f.resumeAfterMetadataFetch()

	// Run the SAML auth flow for all pending requests.
	for _, pf := range pending {
		pf.resumeAfterMetadataFetch()
	}
}

// resumeAfterMetadataFetch runs the SAML authentication flow after IdP metadata
// has been loaded via HttpCallout. It is called from the callout callback for both
// the originating request and any pending requests. Unlike OnRequestHeaders, this
// runs outside the filter chain, so ContinueRequest() must be called explicitly
// when the request should proceed to upstream.
func (f *samlHTTPFilter) resumeAfterMetadataFetch() {
	status := f.processAuthenticatedRequest(f.requestHeaders, f.headerEndStream)
	// ContinueRequest is needed when the request should proceed through the filter chain:
	// - HeadersStatusContinue: valid session, forward to upstream with identity headers.
	// - ACS POST with body (isACSRequest && !headerEndStream): resume so OnRequestBody is invoked.
	// For all other cases, SendLocalResponse was already called (redirects, errors, metadata).
	if status == shared.HeadersStatusContinue || (f.isACSRequest && !f.headerEndStream) {
		f.handle.ContinueRequest()
	}
}

// failPendingRequests sends a 503 to the originating request and all pending requests,
// and resets the fetching state to allow retry on subsequent requests.
// Must be called with f.cfg.mu held. Unlocks the mutex before returning.
func (f *samlHTTPFilter) failPendingRequests(msg string) {
	f.cfg.metadataFetching = false
	pending := f.cfg.pendingRequests
	f.cfg.pendingRequests = nil
	f.cfg.mu.Unlock()

	f.handle.SendLocalResponse(503, nil, []byte(msg), "saml")
	for _, pf := range pending {
		pf.handle.SendLocalResponse(503, nil, []byte(msg), "saml")
	}
}

// processAuthenticatedRequest handles the main SAML flow once IdP metadata is available.
func (f *samlHTTPFilter) processAuthenticatedRequest(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	path := getRequestPath(f.handle)
	method := getRequestMethod(f.handle)
	cfg := f.cfg.config
	idpMeta := f.cfg.idpMetadata

	// Serve SP metadata.
	if path == cfg.MetadataPath {
		f.serveMetadata()
		return shared.HeadersStatusStop
	}

	// ACS endpoint — buffer the POST body.
	if path == cfg.ACSPath && strings.EqualFold(method, "POST") {
		f.isACSRequest = true
		f.requestScheme, _ = f.handle.GetAttributeString(shared.AttributeIDRequestScheme)
		f.requestHost, _ = f.handle.GetAttributeString(shared.AttributeIDRequestHost)
		if endStream {
			f.handle.Log(shared.LogLevelError, "saml: [%s] ACS POST with empty body", f.requestID)
			f.handle.SendLocalResponse(400, nil, []byte("Bad Request: empty ACS POST body"), "saml")
			return shared.HeadersStatusStop
		}
		return shared.HeadersStatusStop
	}

	// Check for valid session cookie.
	cookieHeader := headers.GetOne("cookie")
	if cookieHeader != "" {
		token := extractSessionCookie(cookieHeader, cfg.CookieName)
		if token != "" {
			session, err := validateSessionToken(cfg.CookieSigningKey, token)
			if err == nil {
				f.setUpstreamHeaders(headers, session)
				f.handle.Log(shared.LogLevelDebug, "saml: [%s] valid session for %s", f.requestID, session.NameID)
				f.incrementSessionsValidated("valid")
				return shared.HeadersStatusContinue
			}
			f.handle.Log(shared.LogLevelDebug, "saml: [%s] invalid session cookie: %s", f.requestID, err.Error())
			if strings.Contains(err.Error(), "expired") {
				f.incrementSessionsValidated("expired")
			} else {
				f.incrementSessionsValidated("invalid")
			}
		}
	}

	// No valid session — redirect to IdP.
	scheme, _ := f.handle.GetAttributeString(shared.AttributeIDRequestScheme)
	host, _ := f.handle.GetAttributeString(shared.AttributeIDRequestHost)
	originalURL := buildOriginalURL(f.handle)
	redirectURL, err := generateAuthnRequest(f.handle, cfg, idpMeta, scheme, host, originalURL)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "saml: [%s] failed to generate AuthnRequest: %s", f.requestID, err.Error())
		f.handle.SendLocalResponse(500, nil, []byte("Internal Server Error"), "saml")
		return shared.HeadersStatusStop
	}

	f.handle.Log(shared.LogLevelInfo, "saml: [%s] redirecting to IdP for authentication", f.requestID)
	f.handle.Log(shared.LogLevelDebug, "saml: [%s] redirect target: %s", f.requestID, idpMeta.SSOURL)
	f.handle.SendLocalResponse(302, [][2]string{
		{"location", redirectURL},
		{"cache-control", "no-cache, no-store"},
	}, nil, "saml-redirect")
	f.incrementAuthnRequests()

	return shared.HeadersStatusStop
}

// OnRequestBody processes the buffered request body, specifically for the ACS endpoint.
func (f *samlHTTPFilter) OnRequestBody(_ shared.BodyBuffer, endStream bool) shared.BodyStatus {
	if !f.isACSRequest {
		return shared.BodyStatusContinue
	}

	if !endStream {
		// Keep buffering until we have the complete body.
		return shared.BodyStatusStopAndBuffer
	}

	cfg := f.cfg.config
	idpMeta := f.cfg.idpMetadata

	// Access the full buffered body. We use BufferedRequestBody() rather than the
	// body parameter because the latter may only contain the final chunk, not the
	// entire accumulated body from previous StopAndBuffer calls.
	buffered := f.handle.BufferedRequestBody()
	chunks := buffered.GetChunks()
	bodyStr := parseFormBody(chunks)
	f.handle.Log(shared.LogLevelDebug, "saml: [%s] ACS body size: %d bytes", f.requestID, len(bodyStr))

	// Process the ACS POST.
	session, redirectURL, err := handleACSPost(f.handle, cfg, idpMeta, []byte(bodyStr), f.requestScheme, f.requestHost)
	if err != nil {
		publicMsg := err.Error()
		var sve *validationError
		if errors.As(err, &sve) {
			f.handle.Log(shared.LogLevelError, "saml: [%s] ACS processing failed: %s", f.requestID, sve.PrivateErr)
		} else {
			f.handle.Log(shared.LogLevelError, "saml: [%s] ACS processing failed: %s", f.requestID, err.Error())
		}
		f.incrementAssertionsValidated("failure")
		f.handle.SendLocalResponse(401, [][2]string{
			{"content-type", "text/plain"},
		}, []byte("Unauthorized: "+publicMsg), "saml-acs-error")
		return shared.BodyStatusStopNoBuffer
	}
	f.incrementAssertionsValidated("success")

	// Create session token.
	token, err := createSessionToken(cfg.CookieSigningKey, session)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "saml: [%s] failed to create session token: %s", f.requestID, err.Error())
		f.handle.SendLocalResponse(500, nil, []byte("Internal Server Error"), "saml")
		return shared.BodyStatusStopNoBuffer
	}
	f.incrementSessionsCreated()

	// Set cookie and redirect to the original URL.
	cookie := buildSessionCookie(f.handle, cfg, token)
	f.handle.Log(shared.LogLevelInfo, "saml: [%s] authentication successful for %s, redirecting to %s", f.requestID, session.NameID, redirectURL)
	f.handle.SendLocalResponse(302, [][2]string{
		{"location", redirectURL},
		{"set-cookie", cookie},
		{"cache-control", "no-cache, no-store"},
	}, nil, "saml-acs-redirect")

	return shared.BodyStatusStopNoBuffer
}

// setUpstreamHeaders sets the authenticated user's identity headers on the request
// before forwarding to the upstream service.
func (f *samlHTTPFilter) setUpstreamHeaders(headers shared.HeaderMap, session *SessionData) {
	cfg := f.cfg.config

	// Set the subject (NameID) header.
	headers.Set(cfg.SubjectHeader, session.NameID)

	// Set attribute headers.
	for samlAttr, headerName := range cfg.AttributeHeaders {
		if values, ok := session.Attributes[samlAttr]; ok && len(values) > 0 {
			// Join multiple values with comma.
			headers.Set(headerName, strings.Join(values, ","))
		}
	}
}

// serveMetadata serves the SP metadata XML.
func (f *samlHTTPFilter) serveMetadata() {
	cfg := f.cfg.config
	idpMeta := f.cfg.idpMetadata

	metadataXML, err := generateSPMetadata(cfg, idpMeta)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "saml: [%s] failed to generate SP metadata: %s", f.requestID, err.Error())
		f.handle.SendLocalResponse(500, nil, []byte("Internal Server Error"), "saml")
		return
	}

	f.handle.SendLocalResponse(200, [][2]string{
		{"content-type", "application/samlmetadata+xml"},
		{"cache-control", "public, max-age=3600"},
	}, metadataXML, "saml-metadata")
}

// getRequestPath extracts the request path from attributes.
func getRequestPath(handle shared.HttpFilterHandle) string {
	path, _ := handle.GetAttributeString(shared.AttributeIDRequestPath)
	// Strip query string if present.
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	return path
}

// getRequestMethod extracts the HTTP method from attributes.
func getRequestMethod(handle shared.HttpFilterHandle) string {
	method, _ := handle.GetAttributeString(shared.AttributeIDRequestMethod)
	return method
}

// incrementAuthnRequests increments the authn requests counter.
func (f *samlHTTPFilter) incrementAuthnRequests() {
	if m := f.cfg.metrics; m != nil && m.hasAuthnRequests {
		f.handle.IncrementCounterValue(m.authnRequests, 1)
	}
}

// incrementAssertionsValidated increments the assertions validated counter with a result tag.
func (f *samlHTTPFilter) incrementAssertionsValidated(result string) {
	if m := f.cfg.metrics; m != nil && m.hasAssertionsValidated {
		f.handle.IncrementCounterValue(m.assertionsValidated, 1, result)
	}
}

// incrementSessionsCreated increments the sessions created counter.
func (f *samlHTTPFilter) incrementSessionsCreated() {
	if m := f.cfg.metrics; m != nil && m.hasSessionsCreated {
		f.handle.IncrementCounterValue(m.sessionsCreated, 1)
	}
}

// incrementSessionsValidated increments the sessions validated counter with a result tag.
func (f *samlHTTPFilter) incrementSessionsValidated(result string) {
	if m := f.cfg.metrics; m != nil && m.hasSessionsValidated {
		f.handle.IncrementCounterValue(m.sessionsValidated, 1, result)
	}
}

// buildOriginalURL reconstructs the original request URL from attributes.
func buildOriginalURL(handle shared.HttpFilterHandle) string {
	scheme, _ := handle.GetAttributeString(shared.AttributeIDRequestScheme)
	host, _ := handle.GetAttributeString(shared.AttributeIDRequestHost)
	path, _ := handle.GetAttributeString(shared.AttributeIDRequestPath)

	if scheme == "" {
		scheme = "https"
	}

	u := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   path,
	}
	return u.String()
}

// buildMetadataCalloutHeaders builds the pseudo-headers required for an HttpCallout
// from the IdP metadata URL.
func buildMetadataCalloutHeaders(rawURL string) ([][2]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, errors.New("missing host in URL")
	}

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	return [][2]string{
		{":method", "GET"},
		{":path", path},
		{":authority", u.Host},
	}, nil
}

// getCalloutResponseStatus extracts the :status pseudo-header from callout response headers.
func getCalloutResponseStatus(headers [][2]string) string {
	for _, h := range headers {
		if h[0] == ":status" {
			return h[1]
		}
	}
	return ""
}

// concatBodyChunks concatenates body chunks from an HttpCallout response into a single byte slice.
func concatBodyChunks(chunks [][]byte) []byte {
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) == 1 {
		return chunks[0]
	}
	var buf bytes.Buffer
	for _, chunk := range chunks {
		buf.Write(chunk)
	}
	return buf.Bytes()
}
