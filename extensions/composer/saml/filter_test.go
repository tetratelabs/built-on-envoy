// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"fmt"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newTestFilter creates a samlHttpFilter with a mock handle and test config.
func newTestFilter(t *testing.T) (*samlHTTPFilter, *mocks.MockHttpFilterHandle, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	filterCfg := testFilterConfig(spKP, idpKP)

	f := &samlHTTPFilter{
		handle: handle,
		cfg:    filterCfg,
	}
	return f, handle, ctrl
}

// expectGetAttribute sets up expectations for GetAttributeString calls.
func expectGetAttribute(handle *mocks.MockHttpFilterHandle, path, method, scheme, host string) {
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).Return(path, true).AnyTimes()
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestMethod).Return(method, true).AnyTimes()
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestScheme).Return(scheme, true).AnyTimes()
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestHost).Return(host, true).AnyTimes()
}

func TestOnRequestHeaders_BypassPath(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/health", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_MetadataPath(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/saml/metadata", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	// Expect SendLocalResponse with 200 for metadata.
	handle.EXPECT().SendLocalResponse(
		uint32(200),
		gomock.Any(),
		gomock.Any(),
		gomock.Eq("saml-metadata"),
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_ACSPost_BuffersBody(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/saml/acs", "POST", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	// endStream=false means body is coming.
	status := f.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.True(t, f.isACSRequest)
}

func TestOnRequestHeaders_ACSPost_EmptyBody(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/saml/acs", "POST", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(400), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	// endStream=true means empty POST body.
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_ValidSession(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	cfg := f.cfg.config

	// Create a valid session token.
	session := &SessionData{
		NameID:     "user@example.com",
		Attributes: map[string][]string{"email": {"user@example.com"}, "groups": {"admin"}},
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	}
	token, err := createSessionToken(cfg.CookieSigningKey, session)
	require.NoError(t, err)

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"cookie": {cfg.CookieName + "=" + token},
	})

	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)

	// Verify upstream headers were set.
	require.Equal(t, "user@example.com", headers.GetOne("x-saml-subject"))
	require.Equal(t, "user@example.com", headers.GetOne("x-saml-email"))
	require.Equal(t, "admin", headers.GetOne("x-saml-groups"))
}

func TestOnRequestHeaders_ExpiredSession_Redirects(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	cfg := f.cfg.config

	// Create an expired session token.
	session := &SessionData{
		NameID:    "user@example.com",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	token, err := createSessionToken(cfg.CookieSigningKey, session)
	require.NoError(t, err)

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"cookie": {cfg.CookieName + "=" + token},
	})

	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_NoCookie_Redirects(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_InvalidCookie_Redirects(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	cfg := f.cfg.config

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"cookie": {cfg.CookieName + "=invalid-token-data.bad-sig"},
	})

	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestBody_NonACSRequest(t *testing.T) {
	f, _, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	f.isACSRequest = false
	body := fake.NewFakeBodyBuffer([]byte("some body"))
	status := f.OnRequestBody(body, true)
	require.Equal(t, shared.BodyStatusContinue, status)
}

func TestOnRequestBody_ACS_BufferingUntilEndStream(t *testing.T) {
	f, _, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	f.isACSRequest = true
	body := fake.NewFakeBodyBuffer([]byte("partial"))
	// endStream=false means more body data is coming.
	status := f.OnRequestBody(body, false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, status)
}

func TestOnRequestBody_ACS_InvalidSAMLResponse(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	f.isACSRequest = true

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(
		uint32(401),
		gomock.Any(),
		gomock.Any(),
		gomock.Eq("saml-acs-error"),
	)

	// Provide a body with no SAMLResponse field.
	bufferedBody := fake.NewFakeBodyBuffer([]byte("RelayState=https://sp.example.com/page"))
	handle.EXPECT().BufferedRequestBody().Return(bufferedBody)

	body := fake.NewFakeBodyBuffer(nil)
	status := f.OnRequestBody(body, true)
	require.Equal(t, shared.BodyStatusStopNoBuffer, status)
}

func TestOnRequestHeaders_RedirectContainsIdPSSO(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/dashboard", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	var capturedHeaders [][2]string
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect")).
		Do(func(_ uint32, headers [][2]string, _ []byte, _ string) {
			capturedHeaders = headers
		})

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	f.OnRequestHeaders(headers, true)

	// Verify the Location header points to the IdP SSO URL.
	var location string
	for _, h := range capturedHeaders {
		if h[0] == "location" {
			location = h[1]
		}
	}
	require.Contains(t, location, "idp.example.com/sso")
	require.Contains(t, location, "SAMLRequest=")
}

func TestOnRequestHeaders_ValidSession_MultipleAttributes(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	cfg := f.cfg.config

	session := &SessionData{
		NameID: "admin@example.com",
		Attributes: map[string][]string{
			"email":  {"admin@example.com"},
			"groups": {"admins", "developers"},
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	token, err := createSessionToken(cfg.CookieSigningKey, session)
	require.NoError(t, err)

	expectGetAttribute(handle, "/api/data", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"cookie": {cfg.CookieName + "=" + token},
	})

	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
	// Multi-valued attributes are joined with comma.
	require.Equal(t, "admins,developers", headers.GetOne("x-saml-groups"))
}

func TestFilterFactory_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	filterCfg := testFilterConfig(spKP, idpKP)
	factory := &samlFilterFactory{cfg: filterCfg}

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	filter := factory.Create(handle)
	require.NotNil(t, filter)

	samlFilter, ok := filter.(*samlHTTPFilter)
	require.True(t, ok)
	require.Equal(t, handle, samlFilter.handle)
	require.Equal(t, filterCfg, samlFilter.cfg)
}

func TestConfigFactory_Create_ValidConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	spKP := generateTestKeyPair("sp.example.com")
	idpKP := generateTestKeyPair("idp.example.com")
	idpMeta := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)
	configJSON := testRawConfigJSON(spKP, idpMeta)

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, []byte(configJSON))
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_InvalidConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &HTTPFilterConfigFactory{}
	_, err := factory.Create(configHandle, []byte(`{"entity_id": ""}`))
	require.Error(t, err)
}

func TestConfigFactory_Create_InvalidIdPMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	spKP := generateTestKeyPair("sp.example.com")
	configJSON := testRawConfigJSON(spKP, "<not-valid-idp-metadata/>")

	factory := &HTTPFilterConfigFactory{}
	_, err := factory.Create(configHandle, []byte(configJSON))
	require.Error(t, err)
}

func TestMetrics_AuthnRequests_IncrementedOnRedirect(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	// Expect authnRequests counter (metric ID 1) to be incremented.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestMetrics_AssertionsValidated_Failure(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	f.isACSRequest = true

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(401), gomock.Any(), gomock.Any(), gomock.Eq("saml-acs-error"))

	// Provide a body with no SAMLResponse field.
	bufferedBody := fake.NewFakeBodyBuffer([]byte("RelayState=https://sp.example.com/page"))
	handle.EXPECT().BufferedRequestBody().Return(bufferedBody)

	// Expect assertionsValidated counter (metric ID 2) to be incremented with "failure" tag.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), "failure").Return(shared.MetricsSuccess)

	body := fake.NewFakeBodyBuffer(nil)
	status := f.OnRequestBody(body, true)
	require.Equal(t, shared.BodyStatusStopNoBuffer, status)
}

func TestMetrics_SessionsValidated_Valid(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	cfg := f.cfg.config

	// Create a valid session token.
	session := &SessionData{
		NameID:     "user@example.com",
		Attributes: map[string][]string{"email": {"user@example.com"}},
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	}
	token, err := createSessionToken(cfg.CookieSigningKey, session)
	require.NoError(t, err)

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Expect sessionsValidated counter (metric ID 4) to be incremented with "valid" tag.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(4), uint64(1), "valid").Return(shared.MetricsSuccess)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"cookie": {cfg.CookieName + "=" + token},
	})

	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestMetrics_SessionsValidated_Expired(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	cfg := f.cfg.config

	// Create an expired session token.
	session := &SessionData{
		NameID:    "user@example.com",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	token, err := createSessionToken(cfg.CookieSigningKey, session)
	require.NoError(t, err)

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	// Expect sessionsValidated counter (metric ID 4) to be incremented with "expired" tag.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(4), uint64(1), "expired").Return(shared.MetricsSuccess)
	// Expect authnRequests counter (metric ID 1) to be incremented for the redirect.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"cookie": {cfg.CookieName + "=" + token},
	})

	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

// --- Lazy IdP Metadata Fetch Tests ---

// newTestFilterURLMode creates a filter in URL-metadata mode (idpMetadata is nil).
func newTestFilterURLMode(t *testing.T) (*samlHTTPFilter, *mocks.MockHttpFilterHandle, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	spKP := generateTestKeyPair("sp.example.com")
	filterCfg := testFilterConfigURLMode(spKP)

	f := &samlHTTPFilter{
		handle: handle,
		cfg:    filterCfg,
	}
	return f, handle, ctrl
}

func TestOnRequestHeaders_LazyMetadataFetch_TriggersCallout(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Expect HttpCallout with correct cluster and headers.
	handle.EXPECT().HttpCallout(
		"idp_cluster",
		[][2]string{
			{":method", "GET"},
			{":path", "/metadata"},
			{":authority", "idp.example.com"},
		},
		nil,
		uint64(metadataCalloutTimeoutMs),
		f, // filter is the callback
	).Return(shared.HttpCalloutInitSuccess, uint64(1))

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.True(t, f.cfg.metadataFetching)
}

func TestOnRequestHeaders_LazyMetadataFetch_BypassPathSkipsFetch(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/health", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	// Bypass paths should work even when metadata is not loaded.
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Nil(t, f.cfg.idpMetadata)
}

func TestOnHttpCalloutDone_Success_RunsAuthFlow(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Simulate state after OnRequestHeaders stored headers and triggered fetch.
	f.requestHeaders = fake.NewFakeHeaderMap(map[string][]string{})
	f.headerEndStream = true
	f.cfg.metadataFetching = true

	// Set up attribute expectations for processAuthenticatedRequest.
	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")

	// No session cookie → expect redirect to IdP (not bare ContinueRequest).
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	// Build valid IdP metadata XML.
	idpKP := generateTestKeyPair("idp.example.com")
	metadataXML := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{[]byte(metadataXML)})

	require.NotNil(t, f.cfg.idpMetadata)
	require.Equal(t, "https://idp.example.com", f.cfg.idpMetadata.EntityID)
	require.Equal(t, "https://idp.example.com/sso", f.cfg.idpMetadata.SSOURL)
	require.False(t, f.cfg.metadataFetching)
}

func TestOnHttpCalloutDone_Success_ResumesPendingRequests(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Simulate state after OnRequestHeaders stored headers and triggered fetch.
	f.requestHeaders = fake.NewFakeHeaderMap(map[string][]string{})
	f.headerEndStream = true
	f.cfg.metadataFetching = true

	// Set up expectations for originating request auth flow (redirect, no session).
	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	// Create pending filter instances with their own mock handles.
	pendingHandle1 := mocks.NewMockHttpFilterHandle(ctrl)
	pendingHandle1.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectGetAttribute(pendingHandle1, "/page1", "GET", "https", "sp.example.com")
	pendingHandle1.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))
	pendingHandle1.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	pendingHandle2 := mocks.NewMockHttpFilterHandle(ctrl)
	pendingHandle2.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectGetAttribute(pendingHandle2, "/page2", "GET", "https", "sp.example.com")
	pendingHandle2.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))
	pendingHandle2.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	pendingFilter1 := &samlHTTPFilter{
		handle:          pendingHandle1,
		cfg:             f.cfg,
		requestHeaders:  fake.NewFakeHeaderMap(map[string][]string{}),
		headerEndStream: true,
	}
	pendingFilter2 := &samlHTTPFilter{
		handle:          pendingHandle2,
		cfg:             f.cfg,
		requestHeaders:  fake.NewFakeHeaderMap(map[string][]string{}),
		headerEndStream: true,
	}

	f.cfg.pendingRequests = []*samlHTTPFilter{pendingFilter1, pendingFilter2}

	idpKP := generateTestKeyPair("idp.example.com")
	metadataXML := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{[]byte(metadataXML)})

	require.NotNil(t, f.cfg.idpMetadata)
	require.Empty(t, f.cfg.pendingRequests)
}

func TestOnHttpCalloutDone_CalloutFailure(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	f.cfg.metadataFetching = true

	f.OnHttpCalloutDone(1, shared.HttpCalloutReset, nil, nil)

	require.Nil(t, f.cfg.idpMetadata)
	require.False(t, f.cfg.metadataFetching, "metadataFetching should be reset to allow retry")
}

func TestOnHttpCalloutDone_NonOKStatus(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	f.cfg.metadataFetching = true

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "404"}},
		[][]byte{[]byte("Not Found")})

	require.Nil(t, f.cfg.idpMetadata)
	require.False(t, f.cfg.metadataFetching)
}

func TestOnHttpCalloutDone_EmptyBody(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	f.cfg.metadataFetching = true

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		nil)

	require.Nil(t, f.cfg.idpMetadata)
	require.False(t, f.cfg.metadataFetching)
}

func TestOnHttpCalloutDone_InvalidMetadata(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	f.cfg.metadataFetching = true

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{[]byte("<not-valid-xml/>")})

	require.Nil(t, f.cfg.idpMetadata)
	require.False(t, f.cfg.metadataFetching)
}

func TestOnHttpCalloutDone_Failure_ResumesPendingWith503(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	pendingHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pendingHandle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	pendingFilter := &samlHTTPFilter{handle: pendingHandle, cfg: f.cfg}

	f.cfg.metadataFetching = true
	f.cfg.pendingRequests = []*samlHTTPFilter{pendingFilter}

	f.OnHttpCalloutDone(1, shared.HttpCalloutReset, nil, nil)

	require.Nil(t, f.cfg.idpMetadata)
	require.False(t, f.cfg.metadataFetching)
	require.Empty(t, f.cfg.pendingRequests)
}

func TestOnRequestHeaders_MetadataAlreadyLoaded_SkipsFetch(t *testing.T) {
	// Start with URL-mode config but pre-populate the metadata.
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	idpKP := generateTestKeyPair("idp.example.com")
	f.cfg.idpMetadata = testIDPMetadata(idpKP)

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	// Should proceed to normal auth flow (redirect to IdP since no session).
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_MetadataFetchInProgress_WaitsAsPending(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Simulate another request already fetching metadata.
	f.cfg.metadataFetching = true

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, status)
	require.Len(t, f.cfg.pendingRequests, 1)
	require.Equal(t, f, f.cfg.pendingRequests[0])
}

func TestOnRequestHeaders_HttpCalloutInitFailure(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// HttpCallout returns failure (e.g., cluster not found).
	handle.EXPECT().HttpCallout(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(shared.HttpCalloutInitClusterNotFound, uint64(0))

	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Eq("saml"))

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.False(t, f.cfg.metadataFetching, "metadataFetching should be reset on init failure")
}

func TestOnRequestHeaders_MetadataLoadedAfterLock(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Pre-populate metadata inside the lock to simulate race condition.
	idpKP := generateTestKeyPair("idp.example.com")
	f.cfg.idpMetadata = testIDPMetadata(idpKP)
	// But also set URL to trigger the fetch path check.
	// The outer check (idpMetadata == nil) will not match so it won't enter fetchMetadataOrWait.
	// This test verifies that when metadata is already loaded, normal flow proceeds.
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := f.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnHttpCalloutDone_MultipleBodyChunks(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Simulate state after OnRequestHeaders stored headers and triggered fetch.
	f.requestHeaders = fake.NewFakeHeaderMap(map[string][]string{})
	f.headerEndStream = true
	f.cfg.metadataFetching = true

	// Set up expectations for auth flow after metadata is parsed (redirect, no session).
	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	idpKP := generateTestKeyPair("idp.example.com")
	metadataXML := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	// Split metadata into multiple chunks.
	mid := len(metadataXML) / 2
	chunk1 := []byte(metadataXML[:mid])
	chunk2 := []byte(metadataXML[mid:])

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{chunk1, chunk2})

	require.NotNil(t, f.cfg.idpMetadata)
	require.Equal(t, "https://idp.example.com", f.cfg.idpMetadata.EntityID)
}

func TestOnHttpCalloutDone_Success_ValidSession_ContinuesRequest(t *testing.T) {
	f, handle, ctrl := newTestFilterURLMode(t)
	defer ctrl.Finish()

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// The originating request will be a first-time fetch, so it will have no session
	// and get a redirect. But a pending request can have a valid session cookie.
	f.requestHeaders = fake.NewFakeHeaderMap(map[string][]string{})
	f.headerEndStream = true
	f.cfg.metadataFetching = true

	// Originating request: redirect (no session).
	expectGetAttribute(handle, "/protected", "GET", "https", "sp.example.com")
	handle.EXPECT().SendLocalResponse(uint32(302), gomock.Any(), gomock.Any(), gomock.Eq("saml-redirect"))
	handle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).AnyTimes()

	// Create a pending filter with a valid session cookie.
	idpKP := generateTestKeyPair("idp.example.com")
	metadataXML := testIDPMetadataXML("https://idp.example.com", "https://idp.example.com/sso", idpKP.Cert)

	// Pre-create a session token using the config's signing key.
	session := &SessionData{
		NameID:     "user@example.com",
		Attributes: map[string][]string{"email": {"user@example.com"}},
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	}
	token, err := createSessionToken(f.cfg.config.CookieSigningKey, session)
	require.NoError(t, err)

	pendingHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pendingHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectGetAttribute(pendingHandle, "/dashboard", "GET", "https", "sp.example.com")
	pendingHandle.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	// Valid session → ContinueRequest must be called to forward to upstream.
	pendingHandle.EXPECT().ContinueRequest()

	pendingFilter := &samlHTTPFilter{
		handle: pendingHandle,
		cfg:    f.cfg,
		requestHeaders: fake.NewFakeHeaderMap(map[string][]string{
			"cookie": {f.cfg.config.CookieName + "=" + token},
		}),
		headerEndStream: true,
	}
	f.cfg.pendingRequests = []*samlHTTPFilter{pendingFilter}

	f.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{[]byte(metadataXML)})

	require.NotNil(t, f.cfg.idpMetadata)
}

func TestConfigFactory_Create_URLMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(0), shared.MetricsSuccess).AnyTimes()

	spKP := generateTestKeyPair("sp.example.com")
	configJSON := fmt.Sprintf(`{
		"entity_id": "https://sp.example.com",
		"acs_path": "/saml/acs",
		"idp_metadata_url": "https://idp.example.com/metadata",
		"idp_metadata_cluster": "idp_cluster",
		"sp_cert_pem": %q,
		"sp_key_pem": %q
	}`, spKP.CertPEM, spKP.KeyPEM)

	factory := &HTTPFilterConfigFactory{}
	filterFactory, err := factory.Create(configHandle, []byte(configJSON))
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	// Create a filter and verify it's in URL mode (no metadata yet).
	filterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filter := filterFactory.Create(filterHandle)
	samlFilter := filter.(*samlHTTPFilter)
	require.Nil(t, samlFilter.cfg.idpMetadata)
	require.Equal(t, "https://idp.example.com/metadata", samlFilter.cfg.idpMetadataURL)
	require.Equal(t, "idp_cluster", samlFilter.cfg.idpMetadataCluster)
}

// --- Helpers tests ---

func TestBuildMetadataCalloutHeaders(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    [][2]string
		wantErr bool
	}{
		{
			name: "simple URL",
			url:  "https://idp.example.com/metadata",
			want: [][2]string{
				{":method", "GET"},
				{":path", "/metadata"},
				{":authority", "idp.example.com"},
			},
		},
		{
			name: "URL with port",
			url:  "https://idp.example.com:8443/realms/demo/protocol/saml/descriptor",
			want: [][2]string{
				{":method", "GET"},
				{":path", "/realms/demo/protocol/saml/descriptor"},
				{":authority", "idp.example.com:8443"},
			},
		},
		{
			name: "URL with query string",
			url:  "https://idp.example.com/metadata?format=xml",
			want: [][2]string{
				{":method", "GET"},
				{":path", "/metadata?format=xml"},
				{":authority", "idp.example.com"},
			},
		},
		{
			name:    "missing host",
			url:     "/just-a-path",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers, err := buildMetadataCalloutHeaders(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, headers)
		})
	}
}

func TestGetCalloutResponseStatus(t *testing.T) {
	require.Equal(t, "200", getCalloutResponseStatus([][2]string{{":status", "200"}}))
	require.Equal(t, "404", getCalloutResponseStatus([][2]string{{":status", "404"}, {"content-type", "text/plain"}}))
	require.Empty(t, getCalloutResponseStatus([][2]string{{"content-type", "text/plain"}}))
	require.Empty(t, getCalloutResponseStatus(nil))
}

func TestConcatBodyChunks(t *testing.T) {
	require.Nil(t, concatBodyChunks(nil))
	require.Nil(t, concatBodyChunks([][]byte{}))
	require.Equal(t, []byte("hello"), concatBodyChunks([][]byte{[]byte("hello")}))
	require.Equal(t, []byte("helloworld"), concatBodyChunks([][]byte{[]byte("hello"), []byte("world")}))
}
