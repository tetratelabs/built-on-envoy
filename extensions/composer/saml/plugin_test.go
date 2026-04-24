// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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
		assert.NotNil(t, perRoute.idpMetadata)
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
		perRouteMeta := testIDPMetadata(idpKP2)
		perRoute := &samlFilterConfig{config: perRouteCfg, idpMetadata: perRouteMeta}

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
