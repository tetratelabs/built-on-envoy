// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/crewjam/saml"
)

// IDPMetadata holds the parsed Identity Provider metadata.
type IDPMetadata struct {
	// EntityID is the IdP's entity ID.
	EntityID string
	// SSOURL is the IdP's Single Sign-On URL (HTTP-Redirect binding).
	SSOURL string
	// SLOURL is the IdP's Single Logout URL (HTTP-Redirect binding), if available.
	SLOURL string
	// SigningCerts are the IdP's signing certificates used to verify SAML assertions.
	SigningCerts []*x509.Certificate
	// Descriptor is the full parsed entity descriptor for use with crewjam/saml.
	Descriptor *saml.EntityDescriptor
}

// parseIDPMetadata parses an IdP metadata XML document and extracts the
// relevant fields needed for SAML authentication.
func parseIDPMetadata(data []byte) (*IDPMetadata, error) {
	if len(data) == 0 {
		return nil, errors.New("idp metadata XML is empty")
	}

	var ed saml.EntityDescriptor
	if err := xml.Unmarshal(data, &ed); err != nil {
		return nil, fmt.Errorf("failed to parse idp metadata XML: %w", err)
	}

	if len(ed.IDPSSODescriptors) == 0 {
		return nil, errors.New("idp metadata does not contain an IDPSSODescriptor")
	}

	idpSSO := ed.IDPSSODescriptors[0]

	meta := &IDPMetadata{
		EntityID:   ed.EntityID,
		Descriptor: &ed,
	}

	// Extract SSO URL (prefer HTTP-Redirect binding, fall back to HTTP-POST).
	meta.SSOURL = findSSOURL(idpSSO.SingleSignOnServices)
	if meta.SSOURL == "" {
		return nil, errors.New("idp metadata does not contain a SingleSignOnService URL")
	}

	// Extract SLO URL (optional).
	meta.SLOURL = findSLOURL(idpSSO.SingleLogoutServices)

	// Extract signing certificates.
	certs, err := extractSigningCerts(idpSSO.KeyDescriptors)
	if err != nil {
		return nil, fmt.Errorf("failed to extract idp signing certificates: %w", err)
	}
	meta.SigningCerts = certs

	return meta, nil
}

// findSSOURL finds the SSO URL from the list of SingleSignOnService endpoints.
// It prefers HTTP-Redirect binding, then HTTP-POST binding.
func findSSOURL(services []saml.Endpoint) string {
	var postURL string
	for _, svc := range services {
		switch svc.Binding {
		case saml.HTTPRedirectBinding:
			return svc.Location
		case saml.HTTPPostBinding:
			postURL = svc.Location
		}
	}
	return postURL
}

// findSLOURL finds the SLO URL from the list of SingleLogoutService endpoints.
// It prefers HTTP-Redirect binding, then HTTP-POST binding.
func findSLOURL(services []saml.Endpoint) string {
	var postURL string
	for _, svc := range services {
		switch svc.Binding {
		case saml.HTTPRedirectBinding:
			return svc.Location
		case saml.HTTPPostBinding:
			postURL = svc.Location
		}
	}
	return postURL
}

// extractSigningCerts extracts X.509 certificates from IdP key descriptors.
// It looks for key descriptors with "signing" use, or any key descriptor if
// none are explicitly marked for signing.
func extractSigningCerts(keyDescs []saml.KeyDescriptor) ([]*x509.Certificate, error) {
	// First pass: look for explicit signing key descriptors.
	certs := make([]*x509.Certificate, 0)
	for i := range keyDescs {
		kd := &keyDescs[i]
		if kd.Use == "signing" || kd.Use == "" {
			for _, certData := range kd.KeyInfo.X509Data.X509Certificates {
				cert, err := parseBase64Certificate(certData.Data)
				if err != nil {
					return nil, fmt.Errorf("failed to parse IdP certificate: %w", err)
				}
				certs = append(certs, cert)
			}
		}
	}

	if len(certs) == 0 {
		return nil, errors.New("no signing certificates found in IdP metadata")
	}

	return certs, nil
}

// parseBase64Certificate decodes a base64-encoded DER certificate.
func parseBase64Certificate(data string) (*x509.Certificate, error) {
	// Remove any whitespace (metadata XML often has line-wrapped base64).
	cleaned := strings.ReplaceAll(strings.TrimSpace(data), "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")

	der, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 certificate: %w", err)
	}
	return x509.ParseCertificate(der)
}
