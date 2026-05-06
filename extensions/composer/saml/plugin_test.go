// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"encoding/json"
	"testing"
	"time"

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

func TestConfigFactory_Create_URLMode_SchedulesCallout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	scheduler := mocks.NewMockScheduler(ctrl)
	configHandle.EXPECT().GetScheduler().Return(scheduler)

	scheduledCh := make(chan func(), 1)
	scheduler.EXPECT().Schedule(gomock.Any()).Do(func(fn func()) {
		scheduledCh <- fn
	})

	idpKP := generateTestKeyPair("idp.example.com")
	xml := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

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
	).DoAndReturn(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) (shared.HttpCalloutInitResult, uint64) {
		cb.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
			[][2]shared.UnsafeEnvoyBuffer{
				{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
			},
			[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(xml)},
		)
		return shared.HttpCalloutInitSuccess, uint64(1)
	})

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":                "https://sp.example.com",
		"acs_path":                 "/saml/acs",
		"idp_metadata_url":         "http://idp.example.com:8080/realms/saml-demo/protocol/saml/descriptor",
		"idp_metadata_cluster":     "idp_cluster",
		"idp_metadata_fetch_delay": "0s",
	})

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	sf, ok := filterFactory.(*samlFilterFactory)
	require.True(t, ok)
	require.Nil(t, sf.config.idpMetadata.Load(), "metadata is not loaded yet — fetch hasn't run")

	// Wait for the goroutine to call scheduler.Schedule, then run the scheduled fn
	// (this is what Envoy would do on the config thread). The fn invokes HttpCallout.
	select {
	case fn := <-scheduledCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler.Schedule was not called within 2s")
	}
}

func TestConfigFactory_Create_URLMode_AllInitFailures_GivesUp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	scheduler := mocks.NewMockScheduler(ctrl)
	configHandle.EXPECT().GetScheduler().Return(scheduler).Times(defaultIDPMetadataFetchMaxAttempts)

	scheduledCh := make(chan func(), defaultIDPMetadataFetchMaxAttempts)
	scheduler.EXPECT().Schedule(gomock.Any()).Do(func(fn func()) {
		scheduledCh <- fn
	}).Times(defaultIDPMetadataFetchMaxAttempts)

	configHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(shared.HttpCalloutInitClusterNotFound, uint64(0)).Times(defaultIDPMetadataFetchMaxAttempts)

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":                "https://sp.example.com",
		"acs_path":                 "/saml/acs",
		"idp_metadata_url":         "http://idp.example.com/metadata",
		"idp_metadata_cluster":     "missing_cluster",
		"idp_metadata_fetch_delay": "0s",
	})

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	for i := 0; i < defaultIDPMetadataFetchMaxAttempts; i++ {
		select {
		case fn := <-scheduledCh:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatalf("scheduler.Schedule was not called for attempt %d within 2s", i+1)
		}
	}

	sf, ok := filterFactory.(*samlFilterFactory)
	require.True(t, ok)
	require.Nil(t, sf.config.idpMetadata.Load(), "metadata stays nil when all callout inits fail")
}

func TestConfigFactory_Create_URLMode_CustomMaxAttempts_GivesUp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const customMaxAttempts = 2

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	scheduler := mocks.NewMockScheduler(ctrl)
	configHandle.EXPECT().GetScheduler().Return(scheduler).Times(customMaxAttempts)

	scheduledCh := make(chan func(), customMaxAttempts)
	scheduler.EXPECT().Schedule(gomock.Any()).Do(func(fn func()) {
		scheduledCh <- fn
	}).Times(customMaxAttempts)

	configHandle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(shared.HttpCalloutInitClusterNotFound, uint64(0)).Times(customMaxAttempts)

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":                       "https://sp.example.com",
		"acs_path":                        "/saml/acs",
		"idp_metadata_url":                "http://idp.example.com/metadata",
		"idp_metadata_cluster":            "missing_cluster",
		"idp_metadata_fetch_delay":        "0s",
		"idp_metadata_fetch_max_attempts": customMaxAttempts,
	})

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	for i := 0; i < customMaxAttempts; i++ {
		select {
		case fn := <-scheduledCh:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatalf("scheduler.Schedule was not called for attempt %d within 2s", i+1)
		}
	}

	sf, ok := filterFactory.(*samlFilterFactory)
	require.True(t, ok)
	require.Nil(t, sf.config.idpMetadata.Load(), "metadata stays nil when all callout inits fail")
}

func TestConfigFactory_Create_URLMode_RetriesOnInitFailure_ThenSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	scheduler := mocks.NewMockScheduler(ctrl)
	configHandle.EXPECT().GetScheduler().Return(scheduler).Times(2)

	scheduledCh := make(chan func(), 2)
	scheduler.EXPECT().Schedule(gomock.Any()).Do(func(fn func()) {
		scheduledCh <- fn
	}).Times(2)

	idpKP := generateTestKeyPair("idp.example.com")
	xml := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	gomock.InOrder(
		configHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(shared.HttpCalloutInitClusterNotFound, uint64(0)),
		configHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).DoAndReturn(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) (shared.HttpCalloutInitResult, uint64) {
			cb.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
				[][2]shared.UnsafeEnvoyBuffer{
					{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
				},
				[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(xml)},
			)
			return shared.HttpCalloutInitSuccess, uint64(1)
		}),
	)

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":                "https://sp.example.com",
		"acs_path":                 "/saml/acs",
		"idp_metadata_url":         "http://idp.example.com/metadata",
		"idp_metadata_cluster":     "idp_cluster",
		"idp_metadata_fetch_delay": "0s",
	})

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	for i := 0; i < 2; i++ {
		select {
		case fn := <-scheduledCh:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatalf("scheduler.Schedule was not called for attempt %d within 2s", i+1)
		}
	}

	sf, ok := filterFactory.(*samlFilterFactory)
	require.True(t, ok)
	loaded := sf.config.idpMetadata.Load()
	require.NotNil(t, loaded, "metadata should be loaded after a successful retry")
	require.Equal(t, "https://idp.example.com", loaded.EntityID)
}

func TestConfigFactory_Create_URLMode_RetriesOnCalloutFailure_ThenSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	scheduler := mocks.NewMockScheduler(ctrl)
	configHandle.EXPECT().GetScheduler().Return(scheduler).Times(2)

	scheduledCh := make(chan func(), 2)
	scheduler.EXPECT().Schedule(gomock.Any()).Do(func(fn func()) {
		scheduledCh <- fn
	}).Times(2)

	idpKP := generateTestKeyPair("idp.example.com")
	xml := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	gomock.InOrder(
		configHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).DoAndReturn(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) (shared.HttpCalloutInitResult, uint64) {
			cb.OnHttpCalloutDone(1, shared.HttpCalloutReset, nil, nil)
			return shared.HttpCalloutInitSuccess, uint64(1)
		}),
		configHandle.EXPECT().HttpCallout(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).DoAndReturn(func(_ string, _ [][2]string, _ []byte, _ uint64, cb shared.HttpCalloutCallback) (shared.HttpCalloutInitResult, uint64) {
			cb.OnHttpCalloutDone(2, shared.HttpCalloutSuccess,
				[][2]shared.UnsafeEnvoyBuffer{
					{pkg.UnsafeBufferFromString(":status"), pkg.UnsafeBufferFromString("200")},
				},
				[]shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromString(xml)},
			)
			return shared.HttpCalloutInitSuccess, uint64(2)
		}),
	)

	configJSON, _ := json.Marshal(map[string]any{
		"entity_id":                "https://sp.example.com",
		"acs_path":                 "/saml/acs",
		"idp_metadata_url":         "http://idp.example.com/metadata",
		"idp_metadata_cluster":     "idp_cluster",
		"idp_metadata_fetch_delay": "0s",
	})

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	for i := 0; i < 2; i++ {
		select {
		case fn := <-scheduledCh:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatalf("scheduler.Schedule was not called for attempt %d within 2s", i+1)
		}
	}

	sf, ok := filterFactory.(*samlFilterFactory)
	require.True(t, ok)
	loaded := sf.config.idpMetadata.Load()
	require.NotNil(t, loaded, "metadata should be loaded after a successful retry")
	require.Equal(t, "https://idp.example.com", loaded.EntityID)
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
