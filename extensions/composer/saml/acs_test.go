// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/base64"
	"errors"
	"net/url"
	"testing"

	"github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSafeRedirectURL(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		entityID string
		want     bool
	}{
		{
			name:     "relative path is safe",
			target:   "/app/settings",
			entityID: "https://sp.example.com",
			want:     true,
		},
		{
			name:     "protocol-relative URL is not safe",
			target:   "//evil.com/path",
			entityID: "https://sp.example.com",
			want:     false,
		},
		{
			name:     "same-origin case-insensitive host",
			target:   "https://SP.EXAMPLE.COM/page",
			entityID: "https://sp.example.com",
			want:     true,
		},
		{
			name:     "different host is not safe",
			target:   "https://evil.com/page",
			entityID: "https://sp.example.com",
			want:     false,
		},
		{
			name:     "different scheme is not safe",
			target:   "http://sp.example.com/page",
			entityID: "https://sp.example.com",
			want:     false,
		},
		{
			name:     "invalid target URL returns false",
			target:   "://invalid",
			entityID: "https://sp.example.com",
			want:     false,
		},
		{
			name:     "invalid entity ID returns false",
			target:   "https://sp.example.com/page",
			entityID: "://invalid",
			want:     false,
		},
		{
			name:     "empty target is not safe",
			target:   "",
			entityID: "https://sp.example.com",
			want:     false,
		},
		{
			name:     "root path is safe",
			target:   "/",
			entityID: "https://sp.example.com",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSafeRedirectURL(tt.target, tt.entityID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleACSPost_MissingSAMLResponse(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	body := []byte("RelayState=https://sp.example.com/page")
	_, _, err := handleACSPost(noopLog, cfg, idpMeta, body, "https", "sp.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAMLResponse not found")
}

func TestHandleACSPost_InvalidBody(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	// url.ParseQuery is very lenient, but a body with invalid percent-encoding will fail.
	body := []byte("SAMLResponse=%zz")
	_, _, err := handleACSPost(noopLog, cfg, idpMeta, body, "https", "sp.example.com")
	require.Error(t, err)
}

func TestHandleACSPost_InvalidBase64(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	body := []byte("SAMLResponse=not-valid-base64!!!")
	_, _, err := handleACSPost(noopLog, cfg, idpMeta, body, "https", "sp.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode SAMLResponse")
}

func TestHandleACSPost_InvalidSAMLXML(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	// Encode some invalid XML as a SAMLResponse.
	samlResponse := base64.StdEncoding.EncodeToString([]byte("<not-a-saml-response/>"))
	body := []byte("SAMLResponse=" + url.QueryEscape(samlResponse))
	_, _, err := handleACSPost(noopLog, cfg, idpMeta, body, "https", "sp.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAML assertion validation failed")
}

func TestHandleACSPost_RelayState_SafeRelativePath(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	// We can't create a fully valid signed SAML response in a unit test without
	// complex XML signing, so test the relay state logic indirectly by checking that
	// the code reaches SAML validation (which will fail) but the relay state parsing
	// path is exercised in the successful case via the isSafeRedirectURL tests above.
	// For handleACSPost, we verify the error path with relay state present.
	samlResponse := base64.StdEncoding.EncodeToString([]byte("<not-valid/>"))
	body := []byte("SAMLResponse=" + url.QueryEscape(samlResponse) + "&RelayState=%2Fdashboard")
	_, _, err := handleACSPost(noopLog, cfg, idpMeta, body, "https", "sp.example.com")
	// It will fail at SAML validation, but the parsing up to that point should work.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAML assertion validation failed")
}

func TestExtractAttributes(t *testing.T) {
	tests := []struct {
		name      string
		assertion *saml.Assertion
		want      map[string][]string
	}{
		{
			name:      "empty assertion",
			assertion: &saml.Assertion{},
			want:      map[string][]string{},
		},
		{
			name: "single attribute with single value",
			assertion: &saml.Assertion{
				AttributeStatements: []saml.AttributeStatement{
					{
						Attributes: []saml.Attribute{
							{
								Name: "email",
								Values: []saml.AttributeValue{
									{Value: "user@example.com"},
								},
							},
						},
					},
				},
			},
			want: map[string][]string{
				"email": {"user@example.com"},
			},
		},
		{
			name: "single attribute with multiple values",
			assertion: &saml.Assertion{
				AttributeStatements: []saml.AttributeStatement{
					{
						Attributes: []saml.Attribute{
							{
								Name: "groups",
								Values: []saml.AttributeValue{
									{Value: "admins"},
									{Value: "developers"},
								},
							},
						},
					},
				},
			},
			want: map[string][]string{
				"groups": {"admins", "developers"},
			},
		},
		{
			name: "multiple attributes",
			assertion: &saml.Assertion{
				AttributeStatements: []saml.AttributeStatement{
					{
						Attributes: []saml.Attribute{
							{
								Name: "email",
								Values: []saml.AttributeValue{
									{Value: "user@example.com"},
								},
							},
							{
								Name: "role",
								Values: []saml.AttributeValue{
									{Value: "admin"},
								},
							},
						},
					},
				},
			},
			want: map[string][]string{
				"email": {"user@example.com"},
				"role":  {"admin"},
			},
		},
		{
			name: "falls back to FriendlyName when Name is empty",
			assertion: &saml.Assertion{
				AttributeStatements: []saml.AttributeStatement{
					{
						Attributes: []saml.Attribute{
							{
								FriendlyName: "mail",
								Values: []saml.AttributeValue{
									{Value: "user@example.com"},
								},
							},
						},
					},
				},
			},
			want: map[string][]string{
				"mail": {"user@example.com"},
			},
		},
		{
			name: "attribute with no values is skipped",
			assertion: &saml.Assertion{
				AttributeStatements: []saml.AttributeStatement{
					{
						Attributes: []saml.Attribute{
							{
								Name:   "empty",
								Values: []saml.AttributeValue{},
							},
							{
								Name: "email",
								Values: []saml.AttributeValue{
									{Value: "user@example.com"},
								},
							},
						},
					},
				},
			},
			want: map[string][]string{
				"email": {"user@example.com"},
			},
		},
		{
			name: "multiple attribute statements",
			assertion: &saml.Assertion{
				AttributeStatements: []saml.AttributeStatement{
					{
						Attributes: []saml.Attribute{
							{
								Name:   "email",
								Values: []saml.AttributeValue{{Value: "user@example.com"}},
							},
						},
					},
					{
						Attributes: []saml.Attribute{
							{
								Name:   "role",
								Values: []saml.AttributeValue{{Value: "admin"}},
							},
						},
					},
				},
			},
			want: map[string][]string{
				"email": {"user@example.com"},
				"role":  {"admin"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAttributes(tt.assertion)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidationError(t *testing.T) {
	privateErr := errors.New("signature mismatch")
	ve := &validationError{
		PublicMsg:  "Authentication failed",
		PrivateErr: privateErr,
	}

	assert.Equal(t, "Authentication failed", ve.Error())
	assert.Equal(t, privateErr, ve.Unwrap())
}

func TestExtractSAMLError_WithInvalidResponseError(t *testing.T) {
	privateErr := errors.New("response signature invalid")
	ire := &saml.InvalidResponseError{
		PrivateErr: privateErr,
	}

	result := extractSAMLError(ire)
	assert.Equal(t, "failed to validate SAML response: Authentication failed", result.PublicMsg)
	assert.Equal(t, privateErr, result.PrivateErr)
}

func TestExtractSAMLError_WithInvalidResponseError_NilPrivateErr(t *testing.T) {
	ire := &saml.InvalidResponseError{
		PrivateErr: nil,
	}

	result := extractSAMLError(ire)
	// When PrivateErr is nil, falls through to the generic branch.
	assert.Contains(t, result.PublicMsg, "failed to validate SAML response")
	assert.Equal(t, ire, result.PrivateErr)
}

func TestExtractSAMLError_WithGenericError(t *testing.T) {
	err := errors.New("some generic SAML error")
	result := extractSAMLError(err)
	assert.Equal(t, "failed to validate SAML response: some generic SAML error", result.PublicMsg)
	assert.Equal(t, err, result.PrivateErr)
}

func TestExtractSAMLError_Unwrap(t *testing.T) {
	innerErr := errors.New("inner cause")
	ire := &saml.InvalidResponseError{
		PrivateErr: innerErr,
	}

	result := extractSAMLError(ire)

	// The returned validationError should be unwrappable to the private error.
	var ve *validationError
	require.ErrorAs(t, result, &ve)
	assert.Equal(t, innerErr, errors.Unwrap(ve))
}

func TestBuildServiceProvider(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	sp := buildServiceProvider(cfg, idpMeta, "https", "sp.example.com")

	assert.Equal(t, cfg.EntityID, sp.EntityID)
	assert.Equal(t, "https://sp.example.com/saml/acs", sp.AcsURL.String())
	assert.Equal(t, cfg.SPKey, sp.Key)
	assert.Equal(t, cfg.SPCert, sp.Certificate)
	assert.True(t, sp.AllowIDPInitiated)
	assert.NotNil(t, sp.IDPMetadata)
}

func TestBuildServiceProvider_EmptyScheme(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	sp := buildServiceProvider(cfg, idpMeta, "", "sp.example.com")

	// buildACSURL defaults to https when scheme is empty.
	assert.Equal(t, "https://sp.example.com/saml/acs", sp.AcsURL.String())
}

func TestValidateSAMLResponse_InvalidXML(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	_, err := validateSAMLResponse(cfg, idpMeta, []byte("<invalid/>"), "https", "sp.example.com")
	require.Error(t, err)

	// Should return a validationError.
	var ve *validationError
	require.ErrorAs(t, err, &ve)
}
