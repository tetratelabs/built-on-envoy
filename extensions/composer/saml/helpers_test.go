// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// testKeyPair holds a generated test certificate and private key.
type testKeyPair struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM string
	KeyPEM  string
}

// generateTestKeyPair generates a self-signed certificate and RSA key pair for testing.
func generateTestKeyPair(cn string) *testKeyPair {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("failed to generate RSA key: %v", err))
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(fmt.Sprintf("failed to create certificate: %v", err))
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		panic(fmt.Sprintf("failed to parse certificate: %v", err))
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return &testKeyPair{
		Cert:    cert,
		Key:     key,
		CertPEM: string(certPEM),
		KeyPEM:  string(keyPEM),
	}
}

// testIDPMetadataXML generates an IdP metadata XML document using the given IdP certificate.
func testIDPMetadataXML(idpEntityID, ssoURL string, idpCert *x509.Certificate) string {
	certBase64 := base64.StdEncoding.EncodeToString(idpCert.Raw)
	return fmt.Sprintf(`<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data>
          <X509Certificate>%s</X509Certificate>
        </X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="%s"/>
  </IDPSSODescriptor>
</EntityDescriptor>`, idpEntityID, certBase64, ssoURL)
}

// testRawConfigJSON builds a raw JSON config string for testing parseConfig.
func testRawConfigJSON(spKP *testKeyPair, idpMetadataXML string) string {
	return fmt.Sprintf(`{
		"entity_id": "https://sp.example.com",
		"acs_path": "/saml/acs",
		"idp_metadata_xml": {"inline": %q},
		"sp_cert_pem": {"inline": %q},
		"sp_key_pem": {"inline": %q}
	}`, idpMetadataXML, spKP.CertPEM, spKP.KeyPEM)
}

// testConfig creates a Config struct for testing, with test certificates and sensible defaults.
func testConfig(spKP, idpKP *testKeyPair) *Config {
	signingKey := make([]byte, 32)
	if _, err := rand.Read(signingKey); err != nil {
		panic(fmt.Sprintf("failed to generate signing key: %v", err))
	}

	return &Config{
		EntityID:            "https://sp.example.com",
		ACSPath:             "/saml/acs",
		IDPMetadataXML:      testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert),
		SPCert:              spKP.Cert,
		SPKey:               spKP.Key,
		SPCertPEM:           spKP.CertPEM,
		SPKeyPEM:            spKP.KeyPEM,
		CookieName:          defaultCookieName,
		SessionDuration:     defaultSessionDuration,
		CookieSecure:        defaultCookieSecure,
		CookieSigningKey:    signingKey,
		SubjectHeader:       defaultSubjectHeader,
		SignAuthnRequests:   defaultSignAuthnRequests,
		AllowedClockSkew:    defaultAllowedClockSkew,
		NameIDFormat:        defaultNameIDFormat,
		DefaultRedirectPath: defaultDefaultRedirectURL,
		SLOPath:             defaultSLOPath,
		MetadataPath:        defaultMetadataPath,
		BypassPaths:         []string{"/health", "/ready"},
		AttributeHeaders: map[string]string{
			"email":  "x-saml-email",
			"groups": "x-saml-groups",
		},
	}
}

// testIDPMetadata creates a parsed IdPMetadata struct for testing.
func testIDPMetadata(idpKP *testKeyPair) *IDPMetadata {
	xml := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)
	meta, err := parseIDPMetadata([]byte(xml))
	if err != nil {
		panic(fmt.Sprintf("failed to parse test IdP metadata: %v", err))
	}
	return meta
}

// testFilterConfig creates a samlFilterConfig for filter tests.
func testFilterConfig(spKP, idpKP *testKeyPair) *samlFilterConfig {
	return &samlFilterConfig{
		config:      testConfig(spKP, idpKP),
		idpMetadata: testIDPMetadata(idpKP),
		metrics: &samlMetrics{
			authnRequests:          shared.MetricID(1),
			hasAuthnRequests:       true,
			assertionsValidated:    shared.MetricID(2),
			hasAssertionsValidated: true,
			sessionsCreated:        shared.MetricID(3),
			hasSessionsCreated:     true,
			sessionsValidated:      shared.MetricID(4),
			hasSessionsValidated:   true,
		},
	}
}

var noopLog logger = noopLogger{}

type noopLogger struct{}

func (l noopLogger) Log(shared.LogLevel, string, ...interface{}) {}
