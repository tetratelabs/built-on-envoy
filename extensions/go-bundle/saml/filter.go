// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"errors"
	"net/url"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

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
//  2. SP metadata path → serve metadata XML.
//  3. ACS path (POST) → buffer body for processing in OnRequestBody.
//  4. Valid session cookie → set upstream headers and continue.
//  5. No/invalid session → redirect to IdP with AuthnRequest.
func (f *samlHTTPFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	path := getRequestPath(f.handle)
	method := getRequestMethod(f.handle)
	f.requestID = headers.GetOne("x-request-id")

	f.handle.Log(shared.LogLevelDebug, "saml: [%s] handling %s %s", f.requestID, method, path)

	cfg := f.cfg.config
	idpMeta := f.cfg.idpMetadata

	// 1. Check bypass paths.
	for _, bp := range cfg.BypassPaths {
		if path == bp {
			f.handle.Log(shared.LogLevelDebug, "saml: [%s] bypassing auth for path %s", f.requestID, path)
			return shared.HeadersStatusContinue
		}
	}

	// 2. Serve SP metadata.
	if path == cfg.MetadataPath {
		f.serveMetadata()
		// seveMetadata sends a success or failure local response.
		return shared.HeadersStatusStop
	}

	// 3. ACS endpoint — buffer the POST body.
	if path == cfg.ACSPath && strings.EqualFold(method, "POST") {
		f.isACSRequest = true
		f.requestScheme, _ = f.handle.GetAttributeString(shared.AttributeIDRequestScheme)
		f.requestHost, _ = f.handle.GetAttributeString(shared.AttributeIDRequestHost)
		if endStream {
			// Empty POST body — this shouldn't happen for a valid SAML response.
			f.handle.Log(shared.LogLevelError, "saml: [%s] ACS POST with empty body", f.requestID)
			f.handle.SendLocalResponse(400, nil, []byte("Bad Request: empty ACS POST body"), "saml")
			return shared.HeadersStatusStop
		}
		// Buffer the body; processing happens in OnRequestBody.
		return shared.HeadersStatusStop
	}

	// 4. Check for valid session cookie.
	cookieHeader := headers.GetOne("cookie")
	if cookieHeader != "" {
		token := extractSessionCookie(cookieHeader, cfg.CookieName)
		if token != "" {
			session, err := validateSessionToken(cfg.CookieSigningKey, token)
			if err == nil {
				// Valid session — set upstream headers and continue.
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

	// 5. No valid session — redirect to IdP.
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
