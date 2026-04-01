// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	boeJwe "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt/jwe"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// Helper functions

func getTestKeyPath() string {
	return filepath.Join("jwe", "testdata", "private_key.pem")
}

func getTestPublicKeyPath() string {
	return filepath.Join("jwe", "testdata", "public_key.pem")
}

func getTestSymmetricKey() string {
	// Symmetric shared key (must be 32 bytes for A256KW)
	return "0123456789abcdef0123456789abcdef"
}

func createTestJWE(t *testing.T, payload string) string {
	pubKeyPath := getTestPublicKeyPath()
	keyInput, err := boeJwe.ParsePublicKeyFromFile(pubKeyPath, jwa.RSA_OAEP().String())
	require.NoError(t, err)

	encrypted, err := keyInput.Encrypt([]byte(payload))
	require.NoError(t, err)

	return string(encrypted)
}

func createTestJWEWithSymmetricKey(t *testing.T, payload string) string {
	keyStr := getTestSymmetricKey()
	keyInput, err := boeJwe.ParsePrivateKey(keyStr, jwa.A256KW().String())
	require.NoError(t, err)

	encrypted, err := keyInput.Encrypt([]byte(payload))
	require.NoError(t, err)

	return string(encrypted)
}

// Tests for OnRequestHeaders method

func TestOnRequestHeaders_SuccessfulDecryption(t *testing.T) {
	payload := "test-payload-123"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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
	require.Equal(t, payload, decryptedValues[0].ToUnsafeString())
}

func TestOnRequestHeaders_SuccessfulDecryptionSymmetricKey(t *testing.T) {
	payload := "test-payload-123"
	jweToken := createTestJWEWithSymmetricKey(t, payload)
	inlineKey := base64.StdEncoding.EncodeToString([]byte(getTestSymmetricKey()))

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{Inline: inlineKey},
		Algorithm:    "A256KW",
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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
	require.Equal(t, payload, decryptedValues[0].ToUnsafeString())
}

func TestOnRequestHeaders_WithMetadataOutput(t *testing.T) {
	payload := "test-payload-456"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:  pkg.DataSource{File: getTestKeyPath()},
		Algorithm:   "RSA-OAEP",
		InputHeader: "x-jwe-token",
		OutputMetadata: &pkg.MetadataKey{
			Namespace: "jwe-decrypt",
			Key:       "decrypted-payload",
		},
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	var capturedMetadata []byte
	mockHandle.EXPECT().SetMetadata("jwe-decrypt", "decrypted-payload", gomock.Any()).Do(func(_, _ string, value []byte) {
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
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
		OutputMetadata: &pkg.MetadataKey{
			Namespace: "jwe-decrypt",
			Key:       "decrypted-payload",
		},
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	requestHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(requestHeaders).AnyTimes()

	var capturedMetadata []byte
	mockHandle.EXPECT().SetMetadata("jwe-decrypt", "decrypted-payload", gomock.Any()).Do(func(_, _ string, value []byte) {
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
	require.Equal(t, payload, decryptedValues[0].ToUnsafeString())
	require.Equal(t, []byte(payload), capturedMetadata)
}

func TestOnRequestHeaders_WithCustomMetadataNamespace(t *testing.T) {
	payload := "test-payload-custom-ns"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:  pkg.DataSource{File: getTestKeyPath()},
		Algorithm:   "RSA-OAEP",
		InputHeader: "x-jwe-token",
		OutputMetadata: &pkg.MetadataKey{
			Namespace: "my-custom-namespace",
			Key:       "decrypted-payload",
		},
	}

	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	var capturedMetadata []byte
	mockHandle.EXPECT().SetMetadata("my-custom-namespace", "decrypted-payload", gomock.Any()).Do(func(_, _ string, value []byte) {
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

func TestOnRequestHeaders_NoJWEHeader(t *testing.T) {
	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
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
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
	}

	// Populate the privateKey field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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

func TestOnRequestHeaders_OutputHeaderSingleValue(t *testing.T) {
	payload1 := "first-payload"
	jweToken1 := createTestJWE(t, payload1)

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "authorization",
		OutputHeader: "authorization",
	}

	// Populate the privateKey field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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

	// Multiple JWE tokens in input header
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"authorization": {jweToken1},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("authorization")

	// Using Set() should result in only the last value being present, not multiple values
	// This test verifies that the output header contains only one value (the last one)
	// when processing multiple input JWE tokens
	require.Len(t, decryptedValues, 1, "output header should contain only one value when using Set()")
	require.Equal(t, payload1, decryptedValues[0].ToUnsafeString(), "output header should contain the last decrypted payload")
}

// Tests for prefix handling

func TestOnRequestHeaders_WithPrefix(t *testing.T) {
	payload := "test-payload-with-prefix"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "Authorization",
		OutputHeader: "x-decrypted",
		Prefix:       "Bearer ",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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

	// JWE token with "Bearer " prefix
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"Authorization": {"Bearer " + jweToken},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("x-decrypted")
	require.Len(t, decryptedValues, 1)
	// Should have prefix restored in the output
	require.Equal(t, "Bearer "+payload, decryptedValues[0].ToUnsafeString())
}

func TestOnRequestHeaders_WithPrefixNotMatching(t *testing.T) {
	payload := "test-payload-no-prefix"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "Authorization",
		OutputHeader: "x-decrypted",
		Prefix:       "Bearer ",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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

	// JWE token without the expected prefix
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"Authorization": {jweToken},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("x-decrypted")
	require.Len(t, decryptedValues, 1)
	// Should have prefix added in the output even though input didn't have it
	require.Equal(t, "Bearer "+payload, decryptedValues[0].ToUnsafeString())
}

func TestOnRequestHeaders_WithPrefixShorterThanValue(t *testing.T) {
	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "Authorization",
		OutputHeader: "x-decrypted",
		Prefix:       "Bearer ",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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

	// Value shorter than prefix - should not crash, just fail to decrypt
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"Authorization": {"Bear"},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	// Should have no decrypted values since decryption failed
	decryptedValues := requestHeaders.Get("x-decrypted")
	require.Empty(t, decryptedValues)
}

func TestOnRequestHeaders_WithPrefixAndMetadata(t *testing.T) {
	payload := "test-payload-metadata"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:  pkg.DataSource{File: getTestKeyPath()},
		Algorithm:   "RSA-OAEP",
		InputHeader: "Authorization",
		OutputMetadata: &pkg.MetadataKey{
			Namespace: "jwe-decrypt",
			Key:       "decrypted-payload",
		},
		Prefix: "Bearer ",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{})).AnyTimes()

	var capturedMetadata []byte
	mockHandle.EXPECT().SetMetadata("jwe-decrypt", "decrypted-payload", gomock.Any()).Do(func(_, _ string, value []byte) {
		capturedMetadata = value
	})

	filter := &jweDecryptHttpFilter{
		config: config,
		handle: mockHandle,
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"Authorization": {"Bearer " + jweToken},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	// Metadata should have prefix restored
	require.Equal(t, []byte("Bearer "+payload), capturedMetadata)
}

func TestOnRequestHeaders_WithPrefixMultipleValues(t *testing.T) {
	payload1 := "payload-one"
	payload2 := "payload-two"
	jweToken1 := createTestJWE(t, payload1)
	jweToken2 := createTestJWE(t, payload2)

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "Authorization",
		OutputHeader: "x-decrypted",
		Prefix:       "Bearer ",
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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

	// Multiple JWE tokens with prefix
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"Authorization": {"Bearer " + jweToken1, "Bearer " + jweToken2},
	})

	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	decryptedValues := requestHeaders.Get("x-decrypted")
	// Using Set() means only the last value is retained
	require.Len(t, decryptedValues, 1)
	require.Equal(t, "Bearer "+payload2, decryptedValues[0].ToUnsafeString(), "should contain the last decrypted payload with prefix restored")
}

func TestOnRequestHeaders_WithEmptyPrefix(t *testing.T) {
	payload := "test-payload-empty-prefix"
	jweToken := createTestJWE(t, payload)

	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
		InputHeader:  "x-jwe-token",
		OutputHeader: "x-decrypted",
		Prefix:       "", // Empty prefix should be ignored
	}

	// Populate the privateJwks field
	keySet, err := config.getKey()
	require.NoError(t, err)
	config.privateKey = keySet

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
	// Should not have any prefix added
	require.Equal(t, payload, decryptedValues[0].ToUnsafeString())
}

// Tests for jweDecryptHttpFilterFactory

func TestJweDecryptHttpFilterFactory_Create(t *testing.T) {
	config := &jweDecryptConfig{
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
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
		PrivateKey:   pkg.DataSource{File: getTestKeyPath()},
		Algorithm:    "RSA-OAEP",
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
	require.Equal(t, config.PrivateKey, jweFilterFactory.config.PrivateKey)
	require.Equal(t, config.InputHeader, jweFilterFactory.config.InputHeader)
	require.Equal(t, config.OutputHeader, jweFilterFactory.config.OutputHeader)
}

func TestJWEDecryptHttpFilterConfigFactory_Create_DefaultMetadataNamespace(t *testing.T) {
	// When output_metadata is set without a namespace, it should default to "jwe-decrypt".
	config := jweDecryptConfig{
		PrivateKey:     pkg.DataSource{File: getTestKeyPath()},
		Algorithm:      "RSA-OAEP",
		InputHeader:    "x-jwe-token",
		OutputMetadata: &pkg.MetadataKey{Key: "decrypted-payload"},
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	factory := &JWEDecryptHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	filterFactory, err := factory.Create(mockHandle, configJSON)

	require.NoError(t, err)
	jweFilterFactory, ok := filterFactory.(*jweDecryptHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, "io.builtonenvoy.jwe-decrypt", jweFilterFactory.config.OutputMetadata.Namespace)
}

func TestJWEDecryptHttpFilterConfigFactory_Create_CustomMetadataNamespace(t *testing.T) {
	config := jweDecryptConfig{
		PrivateKey:  pkg.DataSource{File: getTestKeyPath()},
		Algorithm:   "RSA-OAEP",
		InputHeader: "x-jwe-token",
		OutputMetadata: &pkg.MetadataKey{
			Namespace: "my-namespace",
			Key:       "decrypted-payload",
		},
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	factory := &JWEDecryptHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	filterFactory, err := factory.Create(mockHandle, configJSON)

	require.NoError(t, err)
	jweFilterFactory, ok := filterFactory.(*jweDecryptHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, "my-namespace", jweFilterFactory.config.OutputMetadata.Namespace)
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
