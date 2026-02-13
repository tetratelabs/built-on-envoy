// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package jwe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test key paths

func getTestPrivateKeyPath() string {
	return filepath.Join("test", "private_key.pem")
}

func getTestPublicKeyPath() string {
	return filepath.Join("test", "public_key.pem")
}

func readTestPrivateKey(t *testing.T) string {
	keyBytes, err := os.ReadFile(getTestPrivateKeyPath())
	require.NoError(t, err)
	return string(keyBytes)
}

func readTestPublicKey(t *testing.T) string {
	keyBytes, err := os.ReadFile(getTestPublicKeyPath())
	require.NoError(t, err)
	return string(keyBytes)
}

// Tests for ParsePrivateKey

func TestParsePrivateKey_Success(t *testing.T) {
	privateKeyPEM := readTestPrivateKey(t)

	keyInput, err := ParsePrivateKey(privateKeyPEM)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.NotNil(t, keyInput.PrivateKey)
	require.Nil(t, keyInput.PublicKey)
}

func TestParsePrivateKey_EmptyInput(t *testing.T) {
	keyInput, err := ParsePrivateKey("")

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "no key input provided")
}

func TestParsePrivateKey_InvalidPEM(t *testing.T) {
	invalidPEM := "not a valid PEM"

	keyInput, err := ParsePrivateKey(invalidPEM)

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse PEM block")
}

func TestParsePrivateKey_InvalidPKCS8(t *testing.T) {
	// Valid PEM structure but invalid base64 content
	invalidKey := `-----BEGIN PRIVATE KEY-----
aW52YWxpZCBjb250ZW50
-----END PRIVATE KEY-----`

	keyInput, err := ParsePrivateKey(invalidKey)

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse PKCS8 private key")
}

// Tests for ParsePrivateKeyFromFile

func TestParsePrivateKeyFromFile_Success(t *testing.T) {
	keyPath := getTestPrivateKeyPath()

	keyInput, err := ParsePrivateKeyFromFile(keyPath)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.NotNil(t, keyInput.PrivateKey)
	require.Nil(t, keyInput.PublicKey)
}

func TestParsePrivateKeyFromFile_FileNotFound(t *testing.T) {
	keyInput, err := ParsePrivateKeyFromFile("/nonexistent/key.pem")

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to read key file")
}

// Tests for ParsePublicKey

func TestParsePublicKey_Success(t *testing.T) {
	publicKeyPEM := readTestPublicKey(t)

	keyInput, err := ParsePublicKey(publicKeyPEM)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.Nil(t, keyInput.PrivateKey)
	require.NotNil(t, keyInput.PublicKey)
}

func TestParsePublicKey_EmptyInput(t *testing.T) {
	keyInput, err := ParsePublicKey("")

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "no key input provided")
}

func TestParsePublicKey_InvalidPEM(t *testing.T) {
	invalidPEM := "not a valid PEM"

	keyInput, err := ParsePublicKey(invalidPEM)

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse PEM block")
}

func TestParsePublicKey_InvalidPKIX(t *testing.T) {
	// Valid PEM structure but invalid base64 content
	invalidKey := `-----BEGIN PUBLIC KEY-----
aW52YWxpZCBjb250ZW50
-----END PUBLIC KEY-----`

	keyInput, err := ParsePublicKey(invalidKey)

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse PKIX public key")
}

// Tests for ParsePublicKeyFromFile

func TestParsePublicKeyFromFile_Success(t *testing.T) {
	keyPath := getTestPublicKeyPath()

	keyInput, err := ParsePublicKeyFromFile(keyPath)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.Nil(t, keyInput.PrivateKey)
	require.NotNil(t, keyInput.PublicKey)
}

func TestParsePublicKeyFromFile_FileNotFound(t *testing.T) {
	keyInput, err := ParsePublicKeyFromFile("/nonexistent/public_key.pem")

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to read key file")
}

// Tests for ParseKeys

func TestParseKeys_BothKeys(t *testing.T) {
	privateKeyPEM := readTestPrivateKey(t)
	publicKeyPEM := readTestPublicKey(t)

	keyInput, err := ParseKeys(privateKeyPEM, publicKeyPEM)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.NotNil(t, keyInput.PrivateKey)
	require.NotNil(t, keyInput.PublicKey)
}

func TestParseKeys_PrivateKeyOnly(t *testing.T) {
	privateKeyPEM := readTestPrivateKey(t)

	keyInput, err := ParseKeys(privateKeyPEM, "")

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.NotNil(t, keyInput.PrivateKey)
	require.Nil(t, keyInput.PublicKey)
}

func TestParseKeys_PublicKeyOnly(t *testing.T) {
	publicKeyPEM := readTestPublicKey(t)

	keyInput, err := ParseKeys("", publicKeyPEM)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.Nil(t, keyInput.PrivateKey)
	require.NotNil(t, keyInput.PublicKey)
}

func TestParseKeys_EmptyKeys(t *testing.T) {
	keyInput, err := ParseKeys("", "")

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.Nil(t, keyInput.PrivateKey)
	require.Nil(t, keyInput.PublicKey)
}

func TestParseKeys_InvalidPrivateKey(t *testing.T) {
	publicKeyPEM := readTestPublicKey(t)

	keyInput, err := ParseKeys("invalid", publicKeyPEM)

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse private key")
}

func TestParseKeys_InvalidPublicKey(t *testing.T) {
	privateKeyPEM := readTestPrivateKey(t)

	keyInput, err := ParseKeys(privateKeyPEM, "invalid")

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse public key")
}

// Tests for ParseKeysFromFile

func TestParseKeysFromFile_BothKeys(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()
	publicKeyPath := getTestPublicKeyPath()

	keyInput, err := ParseKeysFromFile(privateKeyPath, publicKeyPath)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.NotNil(t, keyInput.PrivateKey)
	require.NotNil(t, keyInput.PublicKey)
}

func TestParseKeysFromFile_PrivateKeyOnly(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()

	keyInput, err := ParseKeysFromFile(privateKeyPath, "")

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.NotNil(t, keyInput.PrivateKey)
	require.Nil(t, keyInput.PublicKey)
}

func TestParseKeysFromFile_PublicKeyOnly(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()

	keyInput, err := ParseKeysFromFile("", publicKeyPath)

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.Nil(t, keyInput.PrivateKey)
	require.NotNil(t, keyInput.PublicKey)
}

func TestParseKeysFromFile_EmptyPaths(t *testing.T) {
	keyInput, err := ParseKeysFromFile("", "")

	require.NoError(t, err)
	require.NotNil(t, keyInput)
	require.Nil(t, keyInput.PrivateKey)
	require.Nil(t, keyInput.PublicKey)
}

func TestParseKeysFromFile_PrivateKeyNotFound(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()

	keyInput, err := ParseKeysFromFile("/nonexistent/private.pem", publicKeyPath)

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse private key from file")
}

func TestParseKeysFromFile_PublicKeyNotFound(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()

	keyInput, err := ParseKeysFromFile(privateKeyPath, "/nonexistent/public.pem")

	require.Error(t, err)
	require.Nil(t, keyInput)
	require.Contains(t, err.Error(), "failed to parse public key from file")
}

// Tests for Encrypt and Decrypt

func TestEncrypt_Success(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()
	keyInput, err := ParsePublicKeyFromFile(publicKeyPath)
	require.NoError(t, err)

	payload := []byte("test payload for encryption")

	encrypted, err := keyInput.Encrypt(payload)

	require.NoError(t, err)
	require.NotNil(t, encrypted)
	require.NotEqual(t, payload, encrypted)
}

func TestEncrypt_EmptyPayload(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()
	keyInput, err := ParsePublicKeyFromFile(publicKeyPath)
	require.NoError(t, err)

	payload := []byte("")

	encrypted, err := keyInput.Encrypt(payload)

	require.NoError(t, err)
	require.NotNil(t, encrypted)
}

func TestEncrypt_WithoutPublicKey(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()
	keyInput, err := ParsePrivateKeyFromFile(privateKeyPath)
	require.NoError(t, err)

	// KeyInput only has private key, no public key
	payload := []byte("test payload")

	encrypted, err := keyInput.Encrypt(payload)

	require.Error(t, err)
	require.Nil(t, encrypted)
}

func TestDecrypt_Success(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()
	publicKeyPath := getTestPublicKeyPath()

	// First encrypt with public key
	pubKeyInput, err := ParsePublicKeyFromFile(publicKeyPath)
	require.NoError(t, err)

	originalPayload := []byte("test payload for round-trip encryption")
	encrypted, err := pubKeyInput.Encrypt(originalPayload)
	require.NoError(t, err)

	// Then decrypt with private key
	privKeyInput, err := ParsePrivateKeyFromFile(privateKeyPath)
	require.NoError(t, err)

	decrypted, err := privKeyInput.Decrypt(encrypted)

	require.NoError(t, err)
	require.NotNil(t, decrypted)
	require.Equal(t, originalPayload, decrypted)
}

func TestDecrypt_InvalidData(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()
	keyInput, err := ParsePrivateKeyFromFile(privateKeyPath)
	require.NoError(t, err)

	invalidEncrypted := []byte("not valid encrypted data")

	decrypted, err := keyInput.Decrypt(invalidEncrypted)

	require.Error(t, err)
	require.Nil(t, decrypted)
}

func TestDecrypt_WithoutPrivateKey(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()
	keyInput, err := ParsePublicKeyFromFile(publicKeyPath)
	require.NoError(t, err)

	// Create some encrypted data first
	payload := []byte("test payload")
	encrypted, err := keyInput.Encrypt(payload)
	require.NoError(t, err)

	// Try to decrypt with only public key (should fail)
	decrypted, err := keyInput.Decrypt(encrypted)

	require.Error(t, err)
	require.Nil(t, decrypted)
}

// Test round-trip encryption/decryption

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	privateKeyPath := getTestPrivateKeyPath()
	publicKeyPath := getTestPublicKeyPath()

	keys, err := ParseKeysFromFile(privateKeyPath, publicKeyPath)
	require.NoError(t, err)

	testCases := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "Simple string",
			payload: []byte("Hello, World!"),
		},
		{
			name:    "JSON payload",
			payload: []byte(`{"key":"value","number":123}`),
		},
		{
			name:    "Long payload",
			payload: []byte("This is a much longer payload that contains more data to test encryption and decryption with longer strings. Lorem ipsum dolor sit amet, consectetur adipiscing elit."),
		},
		{
			name:    "Special characters",
			payload: []byte("Special chars: !@#$%^&*()_+-=[]{}|;':\",./<>?"),
		},
		{
			name:    "Unicode characters",
			payload: []byte("Unicode: 你好世界 🚀 Привет мир"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encrypt with public key
			encrypted, err := keys.Encrypt(tc.payload)
			require.NoError(t, err)
			require.NotNil(t, encrypted)

			// Verify encrypted data is different from original
			if len(tc.payload) > 0 {
				require.NotEqual(t, tc.payload, encrypted)
			}

			// Decrypt with private key
			decrypted, err := keys.Decrypt(encrypted)
			require.NoError(t, err)
			require.NotNil(t, decrypted)

			// Verify decrypted data matches original
			require.Equal(t, tc.payload, decrypted)
		})
	}
}

// Test that multiple encryptions of the same payload produce different ciphertexts

func TestEncrypt_NonDeterministic(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()
	keyInput, err := ParsePublicKeyFromFile(publicKeyPath)
	require.NoError(t, err)

	payload := []byte("same payload for multiple encryptions")

	encrypted1, err := keyInput.Encrypt(payload)
	require.NoError(t, err)

	encrypted2, err := keyInput.Encrypt(payload)
	require.NoError(t, err)

	// The two encrypted values should be different (due to random padding/nonce)
	require.NotEqual(t, encrypted1, encrypted2)
}

// Test that different payloads produce different ciphertexts

func TestEncrypt_DifferentPayloads(t *testing.T) {
	publicKeyPath := getTestPublicKeyPath()
	keyInput, err := ParsePublicKeyFromFile(publicKeyPath)
	require.NoError(t, err)

	payload1 := []byte("first payload")
	payload2 := []byte("second payload")

	encrypted1, err := keyInput.Encrypt(payload1)
	require.NoError(t, err)

	encrypted2, err := keyInput.Encrypt(payload2)
	require.NoError(t, err)

	// Different payloads should produce different encrypted values
	require.NotEqual(t, encrypted1, encrypted2)
}
