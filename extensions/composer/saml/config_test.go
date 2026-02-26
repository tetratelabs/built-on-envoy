// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseConfig_ValidMinimal(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	raw := testRawConfigJSON(spKP, idpMeta)
	cfg, err := parseConfig([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "https://sp.example.com", cfg.EntityID)
	require.Equal(t, "/saml/acs", cfg.ACSPath)
	require.NotNil(t, cfg.SPCert)
	require.NotNil(t, cfg.SPKey)
}

func TestParseConfig_Defaults(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	raw := testRawConfigJSON(spKP, idpMeta)
	cfg, err := parseConfig([]byte(raw))
	require.NoError(t, err)

	require.Equal(t, defaultCookieName, cfg.CookieName)
	require.Equal(t, defaultSessionDuration, cfg.SessionDuration)
	require.Equal(t, defaultCookieSecure, cfg.CookieSecure)
	require.Equal(t, defaultSubjectHeader, cfg.SubjectHeader)
	require.Equal(t, defaultSignAuthnRequests, cfg.SignAuthnRequests)
	require.Equal(t, defaultAllowedClockSkew, cfg.AllowedClockSkew)
	require.Equal(t, defaultNameIDFormat, cfg.NameIDFormat)
	require.Equal(t, defaultDefaultRedirectURL, cfg.DefaultRedirectPath)
	require.Equal(t, defaultSLOPath, cfg.SLOPath)
	require.Equal(t, defaultMetadataPath, cfg.MetadataPath)
	require.Len(t, cfg.CookieSigningKey, 32, "random signing key should be generated")
}

func TestParseConfig_InvalidJSON(t *testing.T) {
	_, err := parseConfig([]byte(`{not json`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse config JSON")
}

func TestParseConfig_MissingRequiredFields(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	tests := []struct {
		name    string
		mutate  func(m map[string]any)
		wantErr string
	}{
		{"missing entity_id", func(m map[string]any) { delete(m, "entity_id") }, "entity_id is required"},
		{"missing acs_path", func(m map[string]any) { delete(m, "acs_path") }, "acs_path is required"},
		{"missing idp_metadata_xml", func(m map[string]any) { delete(m, "idp_metadata_xml") }, "idp_metadata_xml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]any{
				"entity_id":        "https://sp.example.com",
				"acs_path":         "/saml/acs",
				"idp_metadata_xml": map[string]string{"inline": idpMeta},
				"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
				"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
			}
			tt.mutate(m)
			data, _ := json.Marshal(m)
			_, err := parseConfig(data)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseConfig_InvalidCertPEM(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": "<xml/>"},
		"sp_cert_pem":      map[string]string{"inline": "not-a-pem"},
		"sp_key_pem":       map[string]string{"inline": "not-a-pem"},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sp_cert_pem")
}

func TestParseConfig_InvalidKeyPEM(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": "<xml/>"},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":       map[string]string{"inline": "not-a-pem"},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sp_key_pem")
}

func TestParseConfig_SessionOverrides(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)
	secure := false

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
		"session": map[string]any{
			"cookie_name":   "my_session",
			"duration":      "1h",
			"cookie_domain": ".example.com",
			"cookie_secure": secure,
			// 32 bytes = 64 hex chars
			"cookie_signing_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	})
	cfg, err := parseConfig(data)
	require.NoError(t, err)
	require.Equal(t, "my_session", cfg.CookieName)
	require.Equal(t, 1*time.Hour, cfg.SessionDuration)
	require.Equal(t, ".example.com", cfg.CookieDomain)
	require.False(t, cfg.CookieSecure)
	require.Len(t, cfg.CookieSigningKey, 32)
}

func TestParseConfig_InvalidSessionDuration(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
		"session": map[string]any{
			"duration": "not-a-duration",
		},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session duration")
}

func TestParseConfig_InvalidSigningKeyLength(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
		"session": map[string]any{
			"cookie_signing_key": "aabbccdd", // only 4 bytes
		},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "32 bytes")
}

func TestParseConfig_FileFields(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	dir := t.TempDir()
	metaFile := filepath.Join(dir, "idp-metadata.xml")
	certFile := filepath.Join(dir, "sp-cert.pem")
	keyFile := filepath.Join(dir, "sp-key.pem")

	require.NoError(t, os.WriteFile(metaFile, []byte(idpMeta), 0o600))
	require.NoError(t, os.WriteFile(certFile, []byte(spKP.CertPEM), 0o600))
	require.NoError(t, os.WriteFile(keyFile, []byte(spKP.KeyPEM), 0o600))

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"file": metaFile},
		"sp_cert_pem":      map[string]string{"file": certFile},
		"sp_key_pem":       map[string]string{"file": keyFile},
	})
	cfg, err := parseConfig(data)
	require.NoError(t, err)
	require.Equal(t, "https://sp.example.com", cfg.EntityID)
	require.Equal(t, idpMeta, cfg.IDPMetadataXML)
	require.NotNil(t, cfg.SPCert)
	require.NotNil(t, cfg.SPKey)
	require.Equal(t, spKP.CertPEM, cfg.SPCertPEM)
	require.Equal(t, spKP.KeyPEM, cfg.SPKeyPEM)
}

func TestParseConfig_FileAndInline_Error(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	dir := t.TempDir()
	metaFile := filepath.Join(dir, "idp-metadata.xml")
	require.NoError(t, os.WriteFile(metaFile, []byte(idpMeta), 0o600))

	data, _ := json.Marshal(map[string]any{
		"entity_id": "https://sp.example.com",
		"acs_path":  "/saml/acs",
		// Both inline and file set in the same DataSource — should error.
		"idp_metadata_xml": map[string]string{"inline": idpMeta, "file": metaFile},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "idp_metadata_xml")
	require.Contains(t, err.Error(), "only one of")
}

func TestParseConfig_FileMissing_Error(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"file": "/nonexistent/path/metadata.xml"},
		"sp_cert_pem":      map[string]string{"inline": "dummy"},
		"sp_key_pem":       map[string]string{"inline": "dummy"},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "idp_metadata_xml")
}

func TestParseConfig_OptionalOverrides(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)
	signFalse := false

	data, _ := json.Marshal(map[string]any{
		"entity_id":             "https://sp.example.com",
		"acs_path":              "/saml/acs",
		"idp_metadata_xml":      map[string]string{"inline": idpMeta},
		"sp_cert_pem":           map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":            map[string]string{"inline": spKP.KeyPEM},
		"subject_header":        "x-custom-subject",
		"sign_authn_requests":   signFalse,
		"allowed_clock_skew":    "10s",
		"name_id_format":        "urn:oasis:names:tc:SAML:2.0:nameid-format:emailAddress",
		"default_redirect_path": "/dashboard",
		"slo_path":              "/logout",
		"metadata_path":         "/metadata",
		"bypass_paths":          []string{"/ping"},
		"attribute_headers":     map[string]string{"role": "x-role"},
	})
	cfg, err := parseConfig(data)
	require.NoError(t, err)
	require.Equal(t, "x-custom-subject", cfg.SubjectHeader)
	require.False(t, cfg.SignAuthnRequests)
	require.Equal(t, 10*time.Second, cfg.AllowedClockSkew)
	require.Equal(t, "urn:oasis:names:tc:SAML:2.0:nameid-format:emailAddress", cfg.NameIDFormat)
	require.Equal(t, "/dashboard", cfg.DefaultRedirectPath)
	require.Equal(t, "/logout", cfg.SLOPath)
	require.Equal(t, "/metadata", cfg.MetadataPath)
	require.Equal(t, []string{"/ping"}, cfg.BypassPaths)
	require.Equal(t, map[string]string{"role": "x-role"}, cfg.AttributeHeaders)
}

func TestParseConfig_NoCertKey_AutoGenerates(t *testing.T) {
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
	})
	cfg, err := parseConfig(data)
	require.NoError(t, err)
	require.NotNil(t, cfg.SPCert)
	require.NotNil(t, cfg.SPKey)
	require.NotEmpty(t, cfg.SPCertPEM)
	require.NotEmpty(t, cfg.SPKeyPEM)
	require.True(t, cfg.SPCertAutoGenerated)
}

func TestParseConfig_OnlyCert_Error(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sp_cert_pem and sp_key_pem must both be provided, or both omitted")
}

func TestParseConfig_OnlyKey_Error(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
		"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
	})
	_, err := parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sp_cert_pem and sp_key_pem must both be provided, or both omitted")
}

func TestParseConfig_ExplicitCertKey_NotAutoGenerated(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	data, _ := json.Marshal(map[string]any{
		"entity_id":        "https://sp.example.com",
		"acs_path":         "/saml/acs",
		"idp_metadata_xml": map[string]string{"inline": idpMeta},
		"sp_cert_pem":      map[string]string{"inline": spKP.CertPEM},
		"sp_key_pem":       map[string]string{"inline": spKP.KeyPEM},
	})
	cfg, err := parseConfig(data)
	require.NoError(t, err)
	require.False(t, cfg.SPCertAutoGenerated)
	require.NotNil(t, cfg.SPCert)
	require.NotNil(t, cfg.SPKey)
}
