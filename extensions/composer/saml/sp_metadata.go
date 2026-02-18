// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/url"

	"github.com/crewjam/saml"
)

// generateSPMetadata creates the SP metadata XML that can be registered with the IdP.
func generateSPMetadata(cfg *Config, _ *IDPMetadata) ([]byte, error) {
	certBase64 := base64.StdEncoding.EncodeToString(cfg.SPCert.Raw)

	acsURL, _ := url.Parse(cfg.EntityID)
	acsURL.Path = cfg.ACSPath

	nameIDFormat := saml.NameIDFormat(cfg.NameIDFormat)

	trueVal := true
	ed := saml.EntityDescriptor{
		EntityID: cfg.EntityID,
		SPSSODescriptors: []saml.SPSSODescriptor{
			{
				SSODescriptor: saml.SSODescriptor{
					RoleDescriptor: saml.RoleDescriptor{
						ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
						KeyDescriptors: []saml.KeyDescriptor{
							{
								Use: "signing",
								KeyInfo: saml.KeyInfo{
									X509Data: saml.X509Data{
										X509Certificates: []saml.X509Certificate{
											{Data: certBase64},
										},
									},
								},
							},
							{
								Use: "encryption",
								KeyInfo: saml.KeyInfo{
									X509Data: saml.X509Data{
										X509Certificates: []saml.X509Certificate{
											{Data: certBase64},
										},
									},
								},
							},
						},
					},
					SingleLogoutServices: buildSLOEndpoints(cfg),
					NameIDFormats:        []saml.NameIDFormat{nameIDFormat},
				},
				AuthnRequestsSigned:  &trueVal,
				WantAssertionsSigned: &trueVal,
				AssertionConsumerServices: []saml.IndexedEndpoint{
					{
						Binding:   saml.HTTPPostBinding,
						Location:  acsURL.String(),
						Index:     0,
						IsDefault: &trueVal,
					},
				},
			},
		},
	}

	data, err := xml.MarshalIndent(ed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SP metadata: %w", err)
	}

	// Prepend XML declaration.
	header := []byte(xml.Header)
	return append(header, data...), nil
}

// buildSLOEndpoints creates SLO endpoint descriptors if configured.
func buildSLOEndpoints(cfg *Config) []saml.Endpoint {
	if cfg.SLOPath == "" {
		return nil
	}

	sloURL, _ := url.Parse(cfg.EntityID)
	sloURL.Path = cfg.SLOPath

	return []saml.Endpoint{
		{
			Binding:  saml.HTTPRedirectBinding,
			Location: sloURL.String(),
		},
	}
}
