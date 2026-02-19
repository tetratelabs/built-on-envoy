// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"compress/flate"
	"encoding/base64"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateAuthnRequest_Basic(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SignAuthnRequests = false
	idpMeta := testIDPMetadata(idpKP)

	redirectURL, err := generateAuthnRequest(noopLog, cfg, idpMeta, "https", "sp.example.com", "https://sp.example.com/protected")
	require.NoError(t, err)
	require.NotEmpty(t, redirectURL)

	u, err := url.Parse(redirectURL)
	require.NoError(t, err)
	require.Equal(t, "idp.example.com", u.Host)
	require.Equal(t, "/sso", u.Path)
	require.NotEmpty(t, u.Query().Get("SAMLRequest"))
	require.Equal(t, "https://sp.example.com/protected", u.Query().Get("RelayState"))
	// No signature when signing is disabled.
	require.Empty(t, u.Query().Get("Signature"))
	require.Empty(t, u.Query().Get("SigAlg"))
}

func TestGenerateAuthnRequest_WithSigning(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SignAuthnRequests = true
	idpMeta := testIDPMetadata(idpKP)

	redirectURL, err := generateAuthnRequest(noopLog, cfg, idpMeta, "https", "sp.example.com", "https://sp.example.com/page")
	require.NoError(t, err)

	u, err := url.Parse(redirectURL)
	require.NoError(t, err)
	require.NotEmpty(t, u.Query().Get("SAMLRequest"))
	require.NotEmpty(t, u.Query().Get("Signature"))
	require.Equal(t, "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256", u.Query().Get("SigAlg"))
}

func TestGenerateAuthnRequest_DeflatedXML(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SignAuthnRequests = false
	idpMeta := testIDPMetadata(idpKP)

	redirectURL, err := generateAuthnRequest(noopLog, cfg, idpMeta, "https", "sp.example.com", "")
	require.NoError(t, err)

	u, err := url.Parse(redirectURL)
	require.NoError(t, err)

	encoded := u.Query().Get("SAMLRequest")
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	// Inflate the deflated data.
	reader := flate.NewReader(strings.NewReader(string(compressed)))
	defer reader.Close() //nolint:errcheck
	xmlBytes, err := io.ReadAll(reader)
	require.NoError(t, err)

	xml := string(xmlBytes)
	require.Contains(t, xml, "AuthnRequest")
	require.Contains(t, xml, cfg.EntityID)
}

func TestGenerateAuthnRequest_EmptyRelayState(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SignAuthnRequests = false
	idpMeta := testIDPMetadata(idpKP)

	redirectURL, err := generateAuthnRequest(noopLog, cfg, idpMeta, "https", "sp.example.com", "")
	require.NoError(t, err)

	u, err := url.Parse(redirectURL)
	require.NoError(t, err)
	require.Empty(t, u.Query().Get("RelayState"))
}

func TestGenerateAuthnRequest_AbsoluteACSURL(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SignAuthnRequests = false
	idpMeta := testIDPMetadata(idpKP)

	redirectURL, err := generateAuthnRequest(noopLog, cfg, idpMeta, "https", "myapp.example.com", "")
	require.NoError(t, err)

	u, err := url.Parse(redirectURL)
	require.NoError(t, err)

	// Decode and inflate the SAMLRequest to verify the ACS URL.
	encoded := u.Query().Get("SAMLRequest")
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	reader := flate.NewReader(strings.NewReader(string(compressed)))
	defer reader.Close() //nolint:errcheck
	xmlBytes, err := io.ReadAll(reader)
	require.NoError(t, err)

	xml := string(xmlBytes)
	require.Contains(t, xml, `AssertionConsumerServiceURL="https://myapp.example.com/saml/acs"`)
}

func TestBuildACSURL(t *testing.T) {
	cfg := &Config{ACSPath: "/saml/acs"}

	require.Equal(t, "https://app.example.com/saml/acs", buildACSURL(cfg, "https", "app.example.com"))
	require.Equal(t, "http://localhost:8080/saml/acs", buildACSURL(cfg, "http", "localhost:8080"))
	// Empty scheme defaults to https.
	require.Equal(t, "https://app.example.com/saml/acs", buildACSURL(cfg, "", "app.example.com"))
}

func TestGenerateRequestID(t *testing.T) {
	id1, err := generateRequestID()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(id1, "_"), "request ID should start with underscore")

	id2, err := generateRequestID()
	require.NoError(t, err)
	require.NotEqual(t, id1, id2, "request IDs should be unique")
}

func TestDeflateBytes(t *testing.T) {
	input := []byte("Hello, SAML World! This is a test of deflate compression.")
	deflated, err := deflateBytes(input)
	require.NoError(t, err)
	require.NotEmpty(t, deflated)

	// Inflate to verify round-trip.
	reader := flate.NewReader(strings.NewReader(string(deflated)))
	defer reader.Close() //nolint:errcheck
	result, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, input, result)
}
