// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	boeJwe "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt/jwe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Helper functions

func getTestKeyPath() string {
	return filepath.Join("jwe", "test", "private_key.pem")
}

func getTestPublicKeyPath() string {
	return filepath.Join("jwe", "test", "public_key.pem")
}

func readTestPrivateKey(t *testing.T) string {
	keyBytes, err := os.ReadFile(getTestKeyPath())
	require.NoError(t, err)
	return string(keyBytes)
}

func createTestJWE(t *testing.T, payload string) string {
	pubKeyPath := getTestPublicKeyPath()
	keyInput, err := boeJwe.ParsePublicKeyFromFile(pubKeyPath)
	require.NoError(t, err)

	encrypted, err := keyInput.Encrypt([]byte(payload))
	require.NoError(t, err)

	return string(encrypted)
}

// Tests for getKey method

func TestGetKey_WithKeyFile(t *testing.T) {
	config := &jweDecryptConfig{
		KeyFile: getTestKeyPath(),
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	key, err := filter.getKey()
	require.NoError(t, err)
	require.NotNil(t, key)
	require.NotNil(t, key.PrivateKey)
}

func TestGetKey_WithInlineKey(t *testing.T) {
	privateKey := readTestPrivateKey(t)
	encodedKey := base64.StdEncoding.EncodeToString([]byte(privateKey))

	config := &jweDecryptConfig{
		InlineKey: encodedKey,
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	key, err := filter.getKey()
	require.NoError(t, err)
	require.NotNil(t, key)
	require.NotNil(t, key.PrivateKey)
}

func TestGetKey_WithInvalidBase64InlineKey(t *testing.T) {
	config := &jweDecryptConfig{
		InlineKey: "not-valid-base64!!!",
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	key, err := filter.getKey()
	require.Error(t, err)
	require.Nil(t, key)
	require.Contains(t, err.Error(), "failed to base64 decode inline key")
}

func TestGetKey_WithNoKey(t *testing.T) {
	config := &jweDecryptConfig{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	key, err := filter.getKey()
	require.Error(t, err)
	require.Nil(t, key)
	require.Contains(t, err.Error(), "no decryption key provided in config")
}

func TestGetKey_WithNonExistentFile(t *testing.T) {
	config := &jweDecryptConfig{
		KeyFile: "/path/to/nonexistent/key.pem",
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	key, err := filter.getKey()
	require.Error(t, err)
	require.Nil(t, key)
}

// Tests for OnRequestHeaders method

func TestOnRequestHeaders_SuccessfulDecryption(t *testing.T) {
	payload := "test-payload-123"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		KeyFile:      getTestKeyPath(),
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	requestHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(requestHeaders).AnyTimes()
	mockHandle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-jwe-token": {jweToken},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("x-decrypted")
	require.Len(t, decryptedValues, 1)
	require.Equal(t, payload, decryptedValues[0])
}

func TestOnRequestHeaders_WithMetadataOutput(t *testing.T) {
	payload := "test-payload-456"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		KeyFile:           getTestKeyPath(),
		InputHeader:       "x-jwe-token",
		OutputMetadataKey: "decrypted-payload",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	var capturedMetadata []byte
	mockHandle.EXPECT().SetMetadata("jwe-decrypt", "decrypted-payload", gomock.Any()).Do(func(ns, key string, value []byte) {
		capturedMetadata = value
	})

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-jwe-token": {jweToken},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, []byte(payload), capturedMetadata)
}

func TestOnRequestHeaders_WithBothHeaderAndMetadata(t *testing.T) {
	payload := "test-payload-789"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		KeyFile:           getTestKeyPath(),
		InputHeader:       "x-jwe-token",
		OutputHeader:      "x-decrypted",
		OutputMetadataKey: "decrypted-payload",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	requestHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(requestHeaders).AnyTimes()

	var capturedMetadata []byte
	mockHandle.EXPECT().SetMetadata("jwe-decrypt", "decrypted-payload", gomock.Any()).Do(func(ns, key string, value []byte) {
		capturedMetadata = value
	})

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-jwe-token": {jweToken},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("x-decrypted")
	require.Len(t, decryptedValues, 1)
	require.Equal(t, payload, decryptedValues[0])
	require.Equal(t, []byte(payload), capturedMetadata)
}

func TestOnRequestHeaders_NoJWEHeader(t *testing.T) {
	config := &jweDecryptConfig{
		KeyFile:      getTestKeyPath(),
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any())
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_InvalidJWE(t *testing.T) {
	config := &jweDecryptConfig{
		KeyFile:      getTestKeyPath(),
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-jwe-token": {"not-a-valid-jwe-token"},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_MultipleJWEValues(t *testing.T) {
	payload1 := "payload-one"
	payload2 := "payload-two"
	jweToken1 := createTestJWE(t, payload1)
	jweToken2 := createTestJWE(t, payload2)

	config := &jweDecryptConfig{
		KeyFile:      getTestKeyPath(),
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	requestHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(requestHeaders).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-jwe-token": {jweToken1, jweToken2},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("x-decrypted")
	require.Len(t, decryptedValues, 2)
	require.Contains(t, decryptedValues, payload1)
	require.Contains(t, decryptedValues, payload2)
}

func TestOnRequestHeaders_KeyError(t *testing.T) {
	config := &jweDecryptConfig{
		KeyFile:      "/nonexistent/key.pem",
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-jwe-token": {"some-jwe-token"},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
}

// Tests for jweDecryptHttpFilterFactory

func TestJweDecryptHttpFilterFactory_Create(t *testing.T) {
	config := &jweDecryptConfig{
		KeyFile:      getTestKeyPath(),
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	factory := &jweDecryptHttpFilterFactory{config: config}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := factory.Create(mockHandle)

	require.NotNil(t, filter)
	jweFilter, ok := filter.(*jweDecryptHttpFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, jweFilter.handle)
	require.Equal(t, config, jweFilter.config)
}

// Tests for JWEDecryptHttpFilterConfigFactory

func TestJWEDecryptHttpFilterConfigFactory_Create_ValidConfig(t *testing.T) {
	config := jweDecryptConfig{
		KeyFile:      getTestKeyPath(),
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	factory := &JWEDecryptHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	filterFactory, err := factory.Create(mockHandle, configJSON)

	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	jweFilterFactory, ok := filterFactory.(*jweDecryptHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, config.KeyFile, jweFilterFactory.config.KeyFile)
	require.Equal(t, config.InputHeader, jweFilterFactory.config.InputHeader)
	require.Equal(t, config.OutputHeader, jweFilterFactory.config.OutputHeader)
}

func TestJWEDecryptHttpFilterConfigFactory_Create_EmptyConfig(t *testing.T) {
	factory := &JWEDecryptHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any())

	filterFactory, err := factory.Create(mockHandle, []byte{})

	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "empty config")
}

func TestJWEDecryptHttpFilterConfigFactory_Create_InvalidJSON(t *testing.T) {
	factory := &JWEDecryptHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any())

	invalidJSON := []byte("{invalid json")

	filterFactory, err := factory.Create(mockHandle, invalidJSON)

	require.Error(t, err)
	require.Nil(t, filterFactory)
}

// Tests for WellKnownHttpFilterConfigFactories

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()

	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "jwe-decrypt")

	factory, ok := factories["jwe-decrypt"].(*JWEDecryptHttpFilterConfigFactory)
	require.True(t, ok)
	require.NotNil(t, factory)
}
