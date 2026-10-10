// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These tests exercise the filter's private-key parsing and decryption with real JWE payloads.
package jwe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/require"
)

func readTestKey(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filename)) //nolint:gosec // Only fixed repository fixture names are supplied by these tests.
	require.NoError(t, err)
	return data
}

func TestParsePrivateKey_Success(t *testing.T) {
	keys, err := ParsePrivateKey(string(readTestKey(t, "private_key.pem")), jwa.RSA_OAEP().String())
	require.NoError(t, err)
	require.NotNil(t, keys.PrivateKey)
}

func TestParsePrivateKey_EmptyInput(t *testing.T) {
	keys, err := ParsePrivateKey("", jwa.RSA_OAEP().String())
	require.ErrorContains(t, err, "no key input provided")
	require.Nil(t, keys)
}

func TestParsePrivateKey_Symmetric(t *testing.T) {
	keys, err := ParsePrivateKey("0123456789abcdef0123456789abcdef", jwa.A256KW().String())
	require.NoError(t, err)
	require.Implements(t, (*jwk.SymmetricKey)(nil), keys.PrivateKey)
}

func TestParsePrivateKey_InvalidPEM(t *testing.T) {
	keys, err := ParsePrivateKey("-----BEGIN PRIVATE KEY-----\naW52YWxpZCBjb250ZW50\n-----END PRIVATE KEY-----", jwa.RSA_OAEP().String())
	require.ErrorContains(t, err, "failed to import private key")
	require.Nil(t, keys)
}

func TestParsePrivateKey_InvalidAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name      string
		algorithm string
		message   string
	}{
		{name: "unknown algorithm", algorithm: "invalid", message: "invalid algorithm specified"},
		{name: "signature algorithm", algorithm: jwa.HS256().String(), message: "algorithm is not a valid key encryption algorithm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := ParsePrivateKey(string(readTestKey(t, "private_key.pem")), tc.algorithm)
			require.ErrorContains(t, err, tc.message)
			require.Nil(t, keys)
		})
	}
}

func TestDecrypt_RoundTrip(t *testing.T) {
	publicKey, err := jwk.ParseKey(readTestKey(t, "public_key.pem"), jwk.WithPEM(true))
	require.NoError(t, err)
	privateKey, err := ParsePrivateKey(string(readTestKey(t, "private_key.pem")), jwa.RSA_OAEP().String())
	require.NoError(t, err)
	symmetricKey, err := ParsePrivateKey("0123456789abcdef0123456789abcdef", jwa.A256KW().String())
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		algorithm     jwa.KeyEncryptionAlgorithm
		encryptionKey jwk.Key
		decryptionKey *Keys
	}{
		{name: "RSA", algorithm: jwa.RSA_OAEP(), encryptionKey: publicKey, decryptionKey: privateKey},
		{name: "symmetric", algorithm: jwa.A256KW(), encryptionKey: symmetricKey.PrivateKey, decryptionKey: symmetricKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, payload := range []string{
				"Hello, World!",
				`{"key":"value","number":123}`,
				"This is a much longer payload that contains more data to test decryption with longer strings.",
				"Special chars: !@#$%^&*()_+-=[]{}|;':\"./<>?",
				"Unicode: 你好世界 🚀 Привет мир",
			} {
				t.Run(payload, func(t *testing.T) {
					encrypted, err := jwe.Encrypt([]byte(payload), jwe.WithKey(tc.algorithm, tc.encryptionKey))
					require.NoError(t, err)
					decrypted, err := tc.decryptionKey.Decrypt(encrypted)
					require.NoError(t, err)
					require.Equal(t, payload, string(decrypted))
				})
			}
		})
	}
}

func TestDecrypt_InvalidData(t *testing.T) {
	keys, err := ParsePrivateKey(string(readTestKey(t, "private_key.pem")), jwa.RSA_OAEP().String())
	require.NoError(t, err)
	decrypted, err := keys.Decrypt([]byte("not valid encrypted data"))
	require.ErrorContains(t, err, "failed to parse payload")
	require.Nil(t, decrypted)
}

func TestDecrypt_WithoutPrivateKey(t *testing.T) {
	publicKey, err := jwk.ParseKey(readTestKey(t, "public_key.pem"), jwk.WithPEM(true))
	require.NoError(t, err)
	encrypted, err := jwe.Encrypt([]byte("test payload"), jwe.WithKey(jwa.RSA_OAEP(), publicKey))
	require.NoError(t, err)
	decrypted, err := (&Keys{}).Decrypt(encrypted)
	require.Error(t, err)
	require.Nil(t, decrypted)
}
