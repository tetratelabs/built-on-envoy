// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openapivalidator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const testSpec = `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /users:
    get:
      summary: List users
      parameters:
        - name: limit
          in: query
          required: false
          schema:
            type: integer
      responses:
        "200":
          description: OK
    post:
      summary: Create user
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name:
                  type: string
                email:
                  type: string
      responses:
        "201":
          description: Created
  /users/{id}:
    get:
      summary: Get user by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`

// Helper to create a temporary spec file for testing.
func createTestSpecFile(t *testing.T, spec string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "spec-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(spec)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// Helper to create a filter for testing. The returned mock handle has Log set up
// as AnyTimes. For tests that call validateRequest from OnRequestBody/OnRequestTrailers,
// the caller must set up RequestHeaders() on the mock to return the appropriate headers.
func createTestFilter(t *testing.T, spec string, cfg *openAPIValidatorConfig) (*openAPIValidatorHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()

	if cfg.Spec.File == "" && cfg.Spec.Inline == "" {
		cfg.Spec.File = createTestSpecFile(t, spec)
	}

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := filterFactory.Create(mockHandle)
	oaFilter, ok := filter.(*openAPIValidatorHttpFilter)
	require.True(t, ok)

	return oaFilter, mockHandle
}

// Tests for OpenAPIValidatorHttpFilterConfigFactory.Create

func TestConfigFactory_Create_ValidConfig(t *testing.T) {
	specFile := createTestSpecFile(t, testSpec)

	configJSON, err := json.Marshal(openAPIValidatorConfig{
		Spec: pkg.DataSource{File: specFile},
	})
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_InlineSpec(t *testing.T) {
	configJSON, err := json.Marshal(openAPIValidatorConfig{
		Spec: pkg.DataSource{Inline: testSpec},
	})
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_EmptyConfig(t *testing.T) {
	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, []byte{})
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	mockHTTPHandle := newPluginHandleWithoutPerRouteConfig(ctrl)
	filter := filterFactory.Create(mockHTTPHandle)
	require.IsType(t, &shared.EmptyHttpFilter{}, filter)
}

func TestConfigFactory_Create_InvalidJSON(t *testing.T) {
	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, []byte("{invalid"))
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_MissingSpecFile(t *testing.T) {
	configJSON, err := json.Marshal(openAPIValidatorConfig{})
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "either 'inline' or 'file' must be set")
}

func TestConfigFactory_Create_SpecFileNotFound(t *testing.T) {
	configJSON, err := json.Marshal(openAPIValidatorConfig{
		Spec: pkg.DataSource{File: "/nonexistent/spec.yaml"},
	})
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "failed to read spec")
}

func TestConfigFactory_Create_InvalidSpec(t *testing.T) {
	specFile := createTestSpecFile(t, "this is not a valid openapi spec")

	configJSON, err := json.Marshal(openAPIValidatorConfig{
		Spec: pkg.DataSource{File: specFile},
	})
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_SpecFileReadError(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	cfg := openAPIValidatorConfig{
		Spec: pkg.DataSource{File: nonExistent},
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_CustomConfig(t *testing.T) {
	specFile := createTestSpecFile(t, testSpec)

	cfg := openAPIValidatorConfig{
		Spec:         pkg.DataSource{File: specFile},
		MaxBodyBytes: 2048,
		DryRun:       true,
		DenyResponse: &pkg.LocalResponse{
			Status:  422,
			Body:    "Validation failed",
			Headers: map[string]string{"x-error": "true"},
		},
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	oaFactory, ok := filterFactory.(*openAPIValidatorHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, uint64(2048), oaFactory.config.MaxBodyBytes)
	require.True(t, oaFactory.config.DryRun)
	require.Equal(t, 422, oaFactory.config.DenyResponse.Status)
	require.Equal(t, "Validation failed", oaFactory.config.DenyResponse.Body)
	require.Equal(t, "true", oaFactory.config.DenyResponse.Headers["x-error"])
	require.Len(t, oaFactory.config.denyResponseHeaders, 1)
}

func TestConfigFactory_Create_JSONSpec(t *testing.T) {
	jsonSpec := `{
		"openapi": "3.0.0",
		"info": {"title": "Test", "version": "1.0"},
		"paths": {
			"/ping": {
				"get": {
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`
	specFile := createTestSpecFile(t, jsonSpec)

	configJSON, err := json.Marshal(openAPIValidatorConfig{Spec: pkg.DataSource{File: specFile}})
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

// Tests for OnRequestHeaders

func TestOnRequestHeaders_ValidGetRequest(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/users"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	// validateRequest calls handle.RequestHeaders() to build the http.Request.
	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_ValidGetRequestWithPathParam(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/users/123"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_UnknownPath(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})
	mockHandle.EXPECT().SendLocalResponse(
		uint32(400),
		gomock.Any(),
		gomock.Any(),
		"openapi_validation_failed",
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_UnknownPathAllowUnmatched(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		AllowUnmatchedPaths: true,
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_KnownPathStillValidatedWithAllowUnmatched(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		AllowUnmatchedPaths: true,
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/users"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_WrongMethod(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})
	mockHandle.EXPECT().SendLocalResponse(
		uint32(400),
		gomock.Any(),
		gomock.Any(),
		"openapi_validation_failed",
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"DELETE"},
		":path":      {"/users"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_PostStopsForBody(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	// endOfStream=false, so the filter should stop to wait for the body.
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.NotNil(t, filter.route)
}

func TestOnRequestHeaders_PostEndOfStreamValidatesImmediately(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})
	// POST to /users with endOfStream=true but no body should fail validation
	// because the spec requires a request body.
	mockHandle.EXPECT().SendLocalResponse(
		uint32(400),
		gomock.Any(),
		gomock.Any(),
		"openapi_validation_failed",
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_DefaultScheme(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/users"},
		":authority": {"example.com"},
	})

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

// Tests for OnRequestBody

func TestOnRequestBody_ValidJsonBody(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	body := []byte(`{"name": "alice", "email": "alice@example.com"}`)
	fakeBody := fake.NewFakeBodyBuffer(body)

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBody)
	// Simulate the ReceivedRequestBody being the same as BufferedRequestBody,
	// which can happen due to Envoy's buffering logic.
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBody)

	bodyStatus := filter.OnRequestBody(fakeBody, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

func TestOnRequestBody_InvalidJsonBody(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// Missing required field "name".
	body := []byte(`{"email": "alice@example.com"}`)
	fakeBody := fake.NewFakeBodyBuffer(body)

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBody)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBody)
	mockHandle.EXPECT().SendLocalResponse(
		uint32(400),
		gomock.Any(),
		gomock.Any(),
		"openapi_validation_failed",
	)

	bodyStatus := filter.OnRequestBody(fakeBody, true)
	require.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus)
}

func TestOnRequestBody_BufferingUntilEndOfStream(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// First chunk, not end of stream.
	chunk := fake.NewFakeBodyBuffer([]byte(`{"name"`))
	bodyStatus := filter.OnRequestBody(chunk, false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus)
}

func TestOnRequestBody_WithTrailers(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// Body chunk, not end of stream.
	body := []byte(`{"name": "alice"}`)
	chunk := fake.NewFakeBodyBuffer(body)
	bodyStatus := filter.OnRequestBody(chunk, false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus)

	// Process via trailers.
	fakeBuffered := fake.NewFakeBodyBuffer(body)
	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffered)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBuffered)

	trailers := fake.NewFakeHeaderMap(map[string][]string{})
	trailersStatus := filter.OnRequestTrailers(trailers)
	require.Equal(t, shared.TrailersStatusContinue, trailersStatus)
}

// Tests for body size limits

func TestOnRequestBody_OversizedBody_Reject(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		MaxBodyBytes: 10,
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// Body exceeds 10 bytes.
	body := fake.NewFakeBodyBuffer([]byte(`{"name": "alice", "email": "alice@example.com"}`))
	mockHandle.EXPECT().SendLocalResponse(
		uint32(413),
		gomock.Nil(),
		[]byte("Request body too large"),
		"openapi_body_too_large",
	)

	bodyStatus := filter.OnRequestBody(body, true)
	require.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus)
}

func TestOnRequestBody_NoBodyLimit(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		MaxBodyBytes: 0, // No limit.
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	body := []byte(`{"name": "alice"}`)
	fakeBody := fake.NewFakeBodyBuffer(body)

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBody)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBody)
	bodyStatus := filter.OnRequestBody(fakeBody, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

// Tests for dry-run mode

func TestOnRequestHeaders_DryRunAllowsUnknownPath(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		DryRun: true,
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestBody_DryRunAllowsInvalidBody(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		DryRun: true,
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// Missing required field "name".
	bufferedBody := []byte(`{"email": `)
	receivedBody := []byte(`"alice@example.com"}`)
	fakeBufferedBody := fake.NewFakeBodyBuffer(bufferedBody)
	fakeReceivedBody := fake.NewFakeBodyBuffer(receivedBody)

	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().ReceivedBufferedRequestBody().Return(true).Times(1)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBufferedBody)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeReceivedBody)

	bodyStatus := filter.OnRequestBody(fakeReceivedBody, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

func TestOnRequestBody_DryRunAllowsOversizedBody(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		DryRun:       true,
		MaxBodyBytes: 10,
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/users"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	body := fake.NewFakeBodyBuffer([]byte(`{"name": "alice", "email": "alice@example.com"}`))

	bodyStatus := filter.OnRequestBody(body, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

// Tests for custom deny responses

func TestDenyResponse_CustomStatus(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		DenyResponse: &pkg.LocalResponse{Status: 422},
	})

	var capturedStatus uint32
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("openapi_validation_failed"),
	).Do(func(status uint32, _ [][2]string, _ []byte, _ string) {
		capturedStatus = status
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Equal(t, uint32(422), capturedStatus)
}

func TestDenyResponse_CustomBody(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		DenyResponse: &pkg.LocalResponse{Body: "Custom error message"},
	})

	var capturedBody []byte
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("openapi_validation_failed"),
	).Do(func(_ uint32, _ [][2]string, body []byte, _ string) {
		capturedBody = body
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Equal(t, []byte("Custom error message"), capturedBody)
}

func TestDenyResponse_CustomHeaders(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{
		DenyResponse: &pkg.LocalResponse{Headers: map[string]string{"x-error": "validation-failed"}},
	})

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("openapi_validation_failed"),
	).Do(func(_ uint32, headers [][2]string, _ []byte, _ string) {
		capturedHeaders = headers
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Len(t, capturedHeaders, 1)
	require.Equal(t, "x-error", capturedHeaders[0][0])
	require.Equal(t, "validation-failed", capturedHeaders[0][1])
}

func TestDenyResponse_DefaultStatusAndBody(t *testing.T) {
	filter, mockHandle := createTestFilter(t, testSpec, &openAPIValidatorConfig{})

	var capturedStatus uint32
	var capturedBody []byte
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("openapi_validation_failed"),
	).Do(func(status uint32, _ [][2]string, body []byte, _ string) {
		capturedStatus = status
		capturedBody = body
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/nonexistent"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Equal(t, uint32(400), capturedStatus)
	// Default body is the error message.
	require.NotEmpty(t, capturedBody)
}

// Tests for WellKnownHttpFilterConfigFactories

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()

	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "openapi-validator")

	factory, ok := factories["openapi-validator"].(*OpenAPIValidatorHttpFilterConfigFactory)
	require.True(t, ok)
	require.NotNil(t, factory)
}

// Tests for filterFactory.Create

func TestFilterFactory_Create(t *testing.T) {
	specFile := createTestSpecFile(t, testSpec)
	cfg := openAPIValidatorConfig{
		Spec: pkg.DataSource{File: specFile},
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OpenAPIValidatorHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	filter := filterFactory.Create(mockHandle)

	require.NotNil(t, filter)
	oaFilter, ok := filter.(*openAPIValidatorHttpFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, oaFilter.handle)
}

// Test that already-processed requests are no-ops.

func TestOnRequestBody_AlreadyProcessed(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{})
	filter.requestProcessed = true

	bodyStatus := filter.OnRequestBody(nil, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

func TestOnRequestTrailers_AlreadyProcessed(t *testing.T) {
	filter, _ := createTestFilter(t, testSpec, &openAPIValidatorConfig{})
	filter.requestProcessed = true

	trailers := fake.NewFakeHeaderMap(map[string][]string{})
	trailersStatus := filter.OnRequestTrailers(trailers)
	require.Equal(t, shared.TrailersStatusContinue, trailersStatus)
}

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
	f := &OpenAPIValidatorHttpFilterConfigFactory{}

	t.Run("valid inline spec", func(t *testing.T) {
		cfg := map[string]any{
			"spec": map[string]any{"inline": testSpec},
		}
		b, _ := json.Marshal(cfg)
		result, err := f.CreatePerRoute(b)
		require.NoError(t, err)
		require.NotNil(t, result)
		perRoute, ok := result.(*openAPIValidatorParsedConfig)
		require.True(t, ok)
		assert.NotNil(t, perRoute.router)
		assert.Equal(t, 400, perRoute.DenyResponse.Status)
	})

	t.Run("empty config returns error", func(t *testing.T) {
		result, err := f.CreatePerRoute([]byte{})
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		result, err := f.CreatePerRoute([]byte(`{invalid`))
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("invalid spec returns error", func(t *testing.T) {
		cfg := map[string]any{
			"spec": map[string]any{"inline": "not valid openapi"},
		}
		b, _ := json.Marshal(cfg)
		result, err := f.CreatePerRoute(b)
		require.Error(t, err)
		require.Nil(t, result)
	})

	t.Run("custom deny response status", func(t *testing.T) {
		cfg := map[string]any{
			"spec":          map[string]any{"inline": testSpec},
			"deny_response": map[string]any{"status": 422, "body": "Unprocessable Entity"},
		}
		b, _ := json.Marshal(cfg)
		result, err := f.CreatePerRoute(b)
		require.NoError(t, err)
		perRoute, ok := result.(*openAPIValidatorParsedConfig)
		require.True(t, ok)
		assert.Equal(t, 422, perRoute.DenyResponse.Status)
	})
}

func Test_PerRouteConfigOverride(t *testing.T) {
	// Build a spec that only allows GET /items
	routeSpec := `openapi: "3.0.0"
info:
  title: Route API
  version: "1.0"
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
`
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	baseConfigJSON, _ := json.Marshal(openAPIValidatorConfig{
		Spec: pkg.DataSource{Inline: testSpec},
	})
	baseFilterFactory, err := (&OpenAPIValidatorHttpFilterConfigFactory{}).Create(mockConfigHandle, baseConfigJSON)
	require.NoError(t, err)
	baseFactory := baseFilterFactory.(*openAPIValidatorHttpFilterFactory)

	t.Run("per-route config overrides factory config", func(t *testing.T) {
		perRouteJSON, _ := json.Marshal(map[string]any{
			"spec": map[string]any{"inline": routeSpec},
		})
		perRouteResult, err := (&OpenAPIValidatorHttpFilterConfigFactory{}).CreatePerRoute(perRouteJSON)
		require.NoError(t, err)
		perRoute := perRouteResult.(*openAPIValidatorParsedConfig)

		handle := newPluginHandleWithPerRouteConfig(ctrl, perRoute)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*openAPIValidatorHttpFilter)
		require.True(t, ok)
		assert.Equal(t, perRoute, f.config)
	})

	t.Run("nil per-route config uses factory config", func(t *testing.T) {
		handle := newPluginHandleWithoutPerRouteConfig(ctrl)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*openAPIValidatorHttpFilter)
		require.True(t, ok)
		assert.Equal(t, baseFactory.config, f.config)
	})

	t.Run("wrong type per-route config uses factory config", func(t *testing.T) {
		handle := newPluginHandleWithPerRouteConfig(ctrl, "not-a-per-route-config")
		filter := baseFactory.Create(handle)
		f, ok := filter.(*openAPIValidatorHttpFilter)
		require.True(t, ok)
		assert.Equal(t, baseFactory.config, f.config)
	})
}

// ensure shared used
var _ = shared.MetricsSuccess
