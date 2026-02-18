// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/xml"
	"testing"

	"github.com/crewjam/saml"
	"github.com/stretchr/testify/require"
)

func TestGenerateSPMetadata_ValidXML(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify it's valid XML that can be parsed back.
	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)
}

func TestGenerateSPMetadata_EntityID(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)

	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)
	require.Equal(t, cfg.EntityID, ed.EntityID)
}

func TestGenerateSPMetadata_ACSEndpoint(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)

	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)

	require.Len(t, ed.SPSSODescriptors, 1)
	spDesc := ed.SPSSODescriptors[0]
	require.Len(t, spDesc.AssertionConsumerServices, 1)

	acs := spDesc.AssertionConsumerServices[0]
	require.Equal(t, saml.HTTPPostBinding, acs.Binding)
	require.Contains(t, acs.Location, cfg.ACSPath)
}

func TestGenerateSPMetadata_KeyDescriptors(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)

	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)

	spDesc := ed.SPSSODescriptors[0]
	require.Len(t, spDesc.KeyDescriptors, 2)

	var uses []string
	for _, kd := range spDesc.KeyDescriptors {
		uses = append(uses, kd.Use)
		require.NotEmpty(t, kd.KeyInfo.X509Data.X509Certificates)
	}
	require.Contains(t, uses, "signing")
	require.Contains(t, uses, "encryption")
}

func TestGenerateSPMetadata_SLOEndpoint(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SLOPath = "/saml/slo"
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)

	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)

	spDesc := ed.SPSSODescriptors[0]
	require.NotEmpty(t, spDesc.SingleLogoutServices)
	require.Contains(t, spDesc.SingleLogoutServices[0].Location, "/saml/slo")
	require.Equal(t, saml.HTTPRedirectBinding, spDesc.SingleLogoutServices[0].Binding)
}

func TestGenerateSPMetadata_NoSLOWhenEmpty(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	cfg.SLOPath = ""
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)

	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)

	spDesc := ed.SPSSODescriptors[0]
	require.Empty(t, spDesc.SingleLogoutServices)
}

func TestGenerateSPMetadata_NameIDFormat(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	cfg := testConfig(spKP, idpKP)
	idpMeta := testIDPMetadata(idpKP)

	data, err := generateSPMetadata(cfg, idpMeta)
	require.NoError(t, err)

	var ed saml.EntityDescriptor
	err = xml.Unmarshal(data, &ed)
	require.NoError(t, err)

	spDesc := ed.SPSSODescriptors[0]
	require.Contains(t, spDesc.NameIDFormats, saml.NameIDFormat(cfg.NameIDFormat))
}
