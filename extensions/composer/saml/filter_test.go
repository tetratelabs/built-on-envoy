// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
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
	// Simulate the case where the buffered body is the same as the received body.
	handle.EXPECT().ReceivedRequestBody().Return(bufferedBody)

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
	bufferedBody := fake.NewFakeBodyBuffer([]byte("RelayState="))
	receivedBody := fake.NewFakeBodyBuffer([]byte("https://sp.example.com/page"))
	handle.EXPECT().BufferedRequestBody().Return(bufferedBody)
	handle.EXPECT().ReceivedRequestBody().Return(receivedBody)

	// Expect assertionsValidated counter (metric ID 2) to be incremented with "failure" tag.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), "failure").Return(shared.MetricsSuccess)

	body := fake.NewFakeBodyBuffer(nil)
	status := f.OnRequestBody(body, true)
	require.Equal(t, shared.BodyStatusStopNoBuffer, status)
}

func TestMetrics_AssertionsValidated_Failure_WithTrailers(t *testing.T) {
	f, handle, ctrl := newTestFilter(t)
	defer ctrl.Finish()

	f.isACSRequest = true

	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().SendLocalResponse(uint32(401), gomock.Any(), gomock.Any(), gomock.Eq("saml-acs-error"))

	// Provide a body with no SAMLResponse field.
	bufferedBody := fake.NewFakeBodyBuffer([]byte("RelayState="))
	receivedBody := fake.NewFakeBodyBuffer([]byte("https://sp.example.com/page"))
	handle.EXPECT().BufferedRequestBody().Return(bufferedBody)
	handle.EXPECT().ReceivedRequestBody().Return(receivedBody)

	// Expect assertionsValidated counter (metric ID 2) to be incremented with "failure" tag.
	handle.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), "failure").Return(shared.MetricsSuccess)

	body := fake.NewFakeBodyBuffer(nil)
	status := f.OnRequestBody(body, false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, status)

	// Simulate receiving trailers after body.
	trailers := fake.NewFakeHeaderMap(map[string][]string{})
	trailerStatus := f.OnRequestTrailers(trailers)
	require.Equal(t, shared.TrailersStatusStop, trailerStatus)
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
