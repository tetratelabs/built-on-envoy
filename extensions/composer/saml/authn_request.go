// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"bytes"
	"compress/flate"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// generateAuthnRequest creates a SAML AuthnRequest and returns the full redirect URL
// to send the user to the IdP's SSO endpoint. requestScheme and requestHost are used
// to build the absolute ACS URL (e.g. "https" and "myapp.example.com").
func generateAuthnRequest(l logger, cfg *Config, idpMeta *IDPMetadata, requestScheme, requestHost, originalURL string) (string, error) {
	id, err := generateRequestID()
	if err != nil {
		return "", fmt.Errorf("failed to generate request ID: %w", err)
	}

	now := time.Now().UTC()
	nameIDFormat := cfg.NameIDFormat

	req := saml.AuthnRequest{
		ID:           id,
		Version:      "2.0",
		IssueInstant: now,
		Destination:  idpMeta.SSOURL,
		Issuer: &saml.Issuer{
			Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity",
			Value:  cfg.EntityID,
		},
		NameIDPolicy: &saml.NameIDPolicy{
			Format: &nameIDFormat,
		},
		AssertionConsumerServiceURL: buildACSURL(cfg, requestScheme, requestHost),
	}

	// Convert to etree Element and serialize to XML bytes.
	el := req.Element()
	doc := etree.NewDocument()
	doc.SetRoot(el)

	var xmlBuf bytes.Buffer
	doc.WriteTo(&xmlBuf) //nolint:gosec,errcheck
	xmlBytes := xmlBuf.Bytes()

	// Deflate the XML.
	deflated, err := deflateBytes(xmlBytes)
	if err != nil {
		return "", fmt.Errorf("failed to deflate AuthnRequest: %w", err)
	}

	// Base64 encode.
	encoded := base64.StdEncoding.EncodeToString(deflated)

	// Build the redirect URL.
	redirectURL, err := url.Parse(idpMeta.SSOURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse IdP SSO URL: %w", err)
	}

	q := redirectURL.Query()
	q.Set("SAMLRequest", encoded)
	if originalURL != "" {
		q.Set("RelayState", originalURL)
	}

	// If signing is enabled, sign the query string parameters.
	if cfg.SignAuthnRequests {
		q.Set("SigAlg", "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256")

		// The signature is computed over the serialized query string
		// (SAMLRequest + RelayState + SigAlg, in order).
		signedPayload := buildSignedPayload(q)
		sig, err := signPayload(cfg.SPKey, signedPayload)
		if err != nil {
			return "", fmt.Errorf("failed to sign AuthnRequest: %w", err)
		}
		q.Set("Signature", base64.StdEncoding.EncodeToString(sig))
	}

	l.Log(shared.LogLevelDebug,
		"saml: authn request. SAMLRequest: %s, SAMLRequest(deflate&base64): %s, RelayState: %s, SigAlg: %s, Signature: %s",
		xmlBytes, q.Get("SAMLRequest"), q.Get("RelayState"), q.Get("SigAlg"), q.Get("Signature"))

	redirectURL.RawQuery = q.Encode()
	return redirectURL.String(), nil
}

// buildACSURL constructs the absolute ACS URL from the config and request context.
// The SAML spec requires AssertionConsumerServiceURL to be an absolute URL.
func buildACSURL(cfg *Config, scheme, host string) string {
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + host + cfg.ACSPath
}

// generateRequestID generates a random SAML request ID.
func generateRequestID() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("_%x", b), nil
}

// deflateBytes applies DEFLATE compression to the input bytes.
func deflateBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildSignedPayload constructs the payload to be signed for HTTP-Redirect binding.
// Per the SAML spec, the payload is: SAMLRequest=...&RelayState=...&SigAlg=...
func buildSignedPayload(q url.Values) []byte {
	var payload string
	payload = "SAMLRequest=" + url.QueryEscape(q.Get("SAMLRequest"))
	if rs := q.Get("RelayState"); rs != "" {
		payload += "&RelayState=" + url.QueryEscape(rs)
	}
	payload += "&SigAlg=" + url.QueryEscape(q.Get("SigAlg"))
	return []byte(payload)
}

// signPayload signs the payload using RSA-SHA256.
func signPayload(key *rsa.PrivateKey, payload []byte) ([]byte, error) {
	hash := sha256.Sum256(payload)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
}
