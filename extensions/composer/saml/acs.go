// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// AssertionResult holds the extracted data from a validated SAML assertion.
type AssertionResult struct {
	// NameID is the authenticated user's NameID.
	NameID string
	// Attributes maps SAML attribute names to their values.
	Attributes map[string][]string
}

// handleACSPost processes a SAML ACS POST request body.
// It extracts the SAMLResponse and RelayState, validates the assertion,
// and returns the session data and the redirect URL. requestScheme and requestHost
// are used to construct the absolute ACS URL for destination validation.
func handleACSPost(l logger, cfg *Config, idpMeta *IDPMetadata, body []byte, requestScheme, requestHost string) (*SessionData, string, error) {
	// Parse the application/x-www-form-urlencoded body.
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse ACS POST body: %w", err)
	}

	samlResponseB64 := values.Get("SAMLResponse")
	if samlResponseB64 == "" {
		return nil, "", errors.New("SAMLResponse not found in ACS POST body")
	}

	// Decode the SAML Response from base64.
	samlResponseXML, err := base64.StdEncoding.DecodeString(samlResponseB64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode SAMLResponse: %w", err)
	}

	// Validate the SAML assertion using crewjam/saml's ServiceProvider.
	result, err := validateSAMLResponse(cfg, idpMeta, samlResponseXML, requestScheme, requestHost)
	if err != nil {
		return nil, "", fmt.Errorf("SAML assertion validation failed: %w", err)
	}

	// Create session data.
	sessionData := &SessionData{
		NameID:     result.NameID,
		Attributes: result.Attributes,
		ExpiresAt:  time.Now().Add(cfg.SessionDuration),
	}

	// Determine redirect URL with validation to prevent open redirects.
	relayState := values.Get("RelayState")
	redirectURL := cfg.DefaultRedirectPath
	if relayState != "" {
		if isSafeRedirectURL(relayState, cfg.EntityID) {
			redirectURL = relayState
		}
		// Unsafe RelayState is silently replaced with the default redirect path.
	}

	l.Log(shared.LogLevelDebug, "saml: ACS POST processed successfully. Session data: %s, Redirect URL: %s", sessionData, redirectURL)

	return sessionData, redirectURL, nil
}

// isSafeRedirectURL checks that a RelayState URL is safe to redirect to.
// It allows relative paths and same-origin absolute URLs only.
func isSafeRedirectURL(target, entityID string) bool {
	// Relative paths are always safe.
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
		return true
	}

	// Parse both URLs and compare origins.
	targetURL, err := url.Parse(target)
	if err != nil {
		return false
	}

	entityURL, err := url.Parse(entityID)
	if err != nil {
		return false
	}

	return strings.EqualFold(targetURL.Scheme, entityURL.Scheme) &&
		strings.EqualFold(targetURL.Host, entityURL.Host)
}

// validateSAMLResponse validates a SAML Response XML using crewjam/saml's
// ServiceProvider type. This leverages the library's built-in validation
// including signature verification, audience/destination checks, and time validation.
func validateSAMLResponse(cfg *Config, idpMeta *IDPMetadata, responseXML []byte, requestScheme, requestHost string) (*AssertionResult, error) {
	// Build a crewjam/saml ServiceProvider to use its validation logic.
	sp := buildServiceProvider(cfg, idpMeta, requestScheme, requestHost)

	// Build the absolute ACS URL for destination validation.
	acsURL, _ := url.Parse(buildACSURL(cfg, requestScheme, requestHost))

	// Parse and validate the SAML response.
	// possibleRequestIDs parameter is nil to allow IdP-initiated SSO (AllowIDPInitiated=true).
	assertion, err := sp.ParseXMLResponse(responseXML, nil, *acsURL)
	if err != nil {
		return nil, extractSAMLError(err)
	}

	// Extract NameID.
	var nameID string
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		nameID = assertion.Subject.NameID.Value
	}
	if nameID == "" {
		return nil, errors.New("no NameID found in SAML assertion")
	}

	// Extract attributes.
	attrs := extractAttributes(assertion)

	return &AssertionResult{
		NameID:     nameID,
		Attributes: attrs,
	}, nil
}

// buildServiceProvider constructs a crewjam/saml ServiceProvider from our config.
func buildServiceProvider(cfg *Config, idpMeta *IDPMetadata, requestScheme, requestHost string) saml.ServiceProvider {
	acsURL, _ := url.Parse(buildACSURL(cfg, requestScheme, requestHost))

	sp := saml.ServiceProvider{
		EntityID:          cfg.EntityID,
		AcsURL:            *acsURL,
		IDPMetadata:       idpMeta.Descriptor,
		Key:               cfg.SPKey,
		Certificate:       cfg.SPCert,
		AllowIDPInitiated: true,
	}

	return sp
}

// extractAttributes extracts SAML attributes from an assertion into a map.
func extractAttributes(assertion *saml.Assertion) map[string][]string {
	attrs := make(map[string][]string)
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			name := attr.Name
			if name == "" {
				name = attr.FriendlyName
			}
			var values []string
			for _, v := range attr.Values {
				values = append(values, v.Value)
			}
			if len(values) > 0 {
				attrs[name] = values
			}
		}
	}
	return attrs
}

// validationError wraps a SAML validation failure with both a safe public
// message (for the HTTP response) and a detailed private error (for admin logs).
type validationError struct {
	// PublicMsg is safe to return to the end user.
	PublicMsg string
	// PrivateErr contains the detailed cause for admin logging.
	PrivateErr error
}

func (e *validationError) Error() string { return e.PublicMsg }
func (e *validationError) Unwrap() error { return e.PrivateErr }

// extractSAMLError converts a crewjam/saml error into a SAMLValidationError.
// The library's InvalidResponseError.Error() returns the generic "Authentication
// failed" while the real cause is in PrivateErr.
func extractSAMLError(err error) *validationError {
	var ire *saml.InvalidResponseError
	if errors.As(err, &ire) && ire.PrivateErr != nil {
		return &validationError{
			PublicMsg:  "failed to validate SAML response: Authentication failed",
			PrivateErr: ire.PrivateErr,
		}
	}
	return &validationError{
		PublicMsg:  fmt.Sprintf("failed to validate SAML response: %s", err),
		PrivateErr: err,
	}
}
