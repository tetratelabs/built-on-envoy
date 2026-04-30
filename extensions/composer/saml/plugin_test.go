// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/json"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func newPluginHandleWithoutPerRouteConfig(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func newPluginHandleWithPerRouteConfig(ctrl *gomock.Controller, perRouteConfig any) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(perRouteConfig).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func Test_CreatePerRoute(t *testing.T) {
	f := &HTTPFilterConfigFactory{}
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	t.Run("valid config", func(t *testing.T) {
		configJSON := testRawConfigJSON(spKP, idpMeta)
		result, err := f.CreatePerRoute([]byte(configJSON))
		require.NoError(t, err)
		require.NotNil(t, result)
		perRoute, ok := result.(*samlFilterConfig)
		require.True(t, ok)
		assert.Equal(t, "https://sp.example.com", perRoute.config.EntityID)
		assert.NotNil(t, perRoute.idpMetadata.Load())
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		result, err := f.CreatePerRoute([]byte(`{invalid`))
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("invalid idp metadata returns error", func(t *testing.T) {
		configJSON := testRawConfigJSON(spKP, "<not-valid-idp-metadata/>")
		result, err := f.CreatePerRoute([]byte(configJSON))
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("URL mode is rejected for per-route configs", func(t *testing.T) {
		configJSON, _ := json.Marshal(map[string]any{
			"entity_id":            "https://sp.example.com",
			"acs_path":             "/saml/acs",
			"idp_metadata_url":     "https://idp.example.com/metadata",
			"idp_metadata_cluster": "idp_cluster",
		})
		result, err := f.CreatePerRoute(configJSON)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "idp_metadata_url is not supported in per-route")
	})
}

func TestConfigFactory_Create_URLMode_InitiatesCallout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	configHandle.EXPECT().HttpCallout(
		"idp_cluster",
		[][2]string{
			{":method", "GET"},
			{":path", "/realms/saml-demo/protocol/saml/descriptor"},
			{":authority", "idp.example.com:8080"},
		},
		gomock.Nil(),
		uint64(idpMetadataCalloutTimeoutMs),
		gomock.AssignableToTypeOf(&idpMetadataCallback{}),
	).Return(shared.HttpCalloutInitSuccess, uint64(1))

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":            "https://sp.example.com",
		"acs_path":             "/saml/acs",
		"idp_metadata_url":     "http://idp.example.com:8080/realms/saml-demo/protocol/saml/descriptor",
		"idp_metadata_cluster": "idp_cluster",
	})

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
	// Metadata is not yet loaded.
	sf, ok := filterFactory.(*samlFilterFactory)
	require.True(t, ok)
	require.Nil(t, sf.config.idpMetadata.Load())
}

func TestConfigFactory_Create_URLMode_InitFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(shared.HttpCalloutInitClusterNotFound, uint64(0))

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":            "https://sp.example.com",
		"acs_path":             "/saml/acs",
		"idp_metadata_url":     "http://idp.example.com/metadata",
		"idp_metadata_cluster": "missing_cluster",
	})

	factory := &HTTPFilterConfigFactory{}
	_, err := factory.Create(configHandle, configJSON)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initiate idp metadata callout")
}

func TestConfigFactory_Create_URLMode_InvalidURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":            "https://sp.example.com",
		"acs_path":             "/saml/acs",
		"idp_metadata_url":     "not-a-url-no-scheme",
		"idp_metadata_cluster": "idp_cluster",
	})

	factory := &HTTPFilterConfigFactory{}
	_, err := factory.Create(configHandle, configJSON)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must include scheme and host")
}

func TestIDPMetadataCallback_Success_StoresMetadata(t *testing.T) {
	target := &samlFilterConfig{config: &Config{}}
	cb := &idpMetadataCallback{handle: noopLogger{}, target: target}

	idpKP := generateTestKeyPair("idp.example.com")
	xml := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	cb.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{
			{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
		},
		[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(xml)},
	)

	loaded := target.idpMetadata.Load()
	require.NotNil(t, loaded)
	require.Equal(t, "https://idp.example.com", loaded.EntityID)
	require.Equal(t, "https://idp.example.com/sso", loaded.SSOURL)
}

func TestIDPMetadataCallback_NonOKStatus_LeavesMetadataNil(t *testing.T) {
	target := &samlFilterConfig{config: &Config{}}
	cb := &idpMetadataCallback{handle: noopLogger{}, target: target}

	cb.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{
			{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("404")},
		},
		[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("Not Found")},
	)
	require.Nil(t, target.idpMetadata.Load())
}

func TestIDPMetadataCallback_CalloutReset_LeavesMetadataNil(t *testing.T) {
	target := &samlFilterConfig{config: &Config{}}
	cb := &idpMetadataCallback{handle: noopLogger{}, target: target}

	cb.OnHttpCalloutDone(1, shared.HttpCalloutReset, nil, nil)
	require.Nil(t, target.idpMetadata.Load())
}

func TestIDPMetadataCallback_EmptyBody_LeavesMetadataNil(t *testing.T) {
	target := &samlFilterConfig{config: &Config{}}
	cb := &idpMetadataCallback{handle: noopLogger{}, target: target}

	cb.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{
			{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
		},
		nil,
	)
	require.Nil(t, target.idpMetadata.Load())
}

func TestIDPMetadataCallback_InvalidXML_LeavesMetadataNil(t *testing.T) {
	target := &samlFilterConfig{config: &Config{}}
	cb := &idpMetadataCallback{handle: noopLogger{}, target: target}

	cb.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]shared.UnsafeEnvoyBuffer{
			{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
		},
		[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString("<not-valid-idp-metadata/>")},
	)
	require.Nil(t, target.idpMetadata.Load())
}

func Test_PerRouteConfigOverride(t *testing.T) {
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	baseFilterCfg, metrics := testFilterConfig(spKP, idpKP)
	baseFactory := &samlFilterFactory{config: baseFilterCfg, metrics: metrics}

	t.Run("per-route config overrides factory config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		spKP2 := generateTestKeyPair("sp2.example.com")
		idpKP2 := generateTestKeyPair("idp2.example.com")
		perRouteCfg := testConfig(spKP2, idpKP2)
		perRouteCfg.EntityID = "https://sp2.example.com"
		perRoute := &samlFilterConfig{config: perRouteCfg}
		perRoute.idpMetadata.Store(testIDPMetadata(idpKP2))

		handle := newPluginHandleWithPerRouteConfig(ctrl, perRoute)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*samlHTTPFilter)
		require.True(t, ok)
		assert.Equal(t, "https://sp2.example.com", f.cfg.config.EntityID)
	})

	t.Run("nil per-route config uses factory config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		handle := newPluginHandleWithoutPerRouteConfig(ctrl)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*samlHTTPFilter)
		require.True(t, ok)
		assert.Equal(t, baseFilterCfg.config.EntityID, f.cfg.config.EntityID)
	})

	t.Run("wrong type per-route config uses factory config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		handle := newPluginHandleWithPerRouteConfig(ctrl, "not-a-per-route-config")
		filter := baseFactory.Create(handle)
		f, ok := filter.(*samlHTTPFilter)
		require.True(t, ok)
		assert.Equal(t, baseFilterCfg.config.EntityID, f.cfg.config.EntityID)
	})
}
