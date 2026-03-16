// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package jwe provides utilities for parsing RSA keys and performing JWE encryption and decryption.
package jwe

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// Keys holds the RSA private and public keys used for JWE encryption and decryption.
type Keys struct {
	PrivateKey jwk.Key
	PublicKey  jwk.Key
}

// ParseKeys takes PEM-encoded RSA private and public key strings, parses them,
// and returns a Keys struct containing the corresponding jwk.Key objects.
func ParseKeys(priv, pub, privAlg, pubAlg string) (*Keys, error) {
	keys := &Keys{}
	if priv != "" {
		privKey, err := ParsePrivateKey(priv, privAlg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		keys.PrivateKey = privKey.PrivateKey
	}
	if pub != "" {
		pubKey, err := ParsePublicKey(pub, pubAlg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %w", err)
		}
		keys.PublicKey = pubKey.PublicKey
	}
	return keys, nil
}

// ParseKeysFromFile reads PEM-encoded RSA private and public keys from the specified files,
func ParseKeysFromFile(privFile string, pubFile string, privAlg string, pubAlg string) (*Keys, error) {
	keys := &Keys{}
	if privFile != "" {
		privKey, err := ParsePrivateKeyFromFile(privFile, privAlg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key from file: %w", err)
		}
		keys.PrivateKey = privKey.PrivateKey
	}
	if pubFile != "" {
		pubKey, err := ParsePublicKeyFromFile(pubFile, pubAlg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key from file: %w", err)
		}
		keys.PublicKey = pubKey.PublicKey
	}
	return keys, nil
}

// ParsePrivateKey takes a PEM-encoded RSA private key string, parses it,
// and returns a Keys struct containing the corresponding jwk.Key object.
func ParsePrivateKey(keyInput, algorithm string) (*Keys, error) {
	if keyInput == "" {
		return nil, fmt.Errorf("no key input provided")
	}

	alg, err := jwa.KeyAlgorithmFrom(algorithm)
	if err != nil {
		return nil, fmt.Errorf("invalid algorithm specified: %w", err)
	}
	encAlg, ok := alg.(jwa.KeyEncryptionAlgorithm)
	if !ok {
		return nil, fmt.Errorf("algorithm is not a valid key encryption algorithm: %w", err)
	}

	var priv jwk.Key
	if encAlg.IsSymmetric() {
		priv, err = jwk.Import([]byte(keyInput))
		if err != nil {
			return nil, fmt.Errorf("failed to import private key: %w", err)
		}
		if _, ok := priv.(jwk.SymmetricKey); !ok {
			return nil, fmt.Errorf("failed to import symmetric private key: %w", err)
		}
	} else {
		priv, err = jwk.ParseKey([]byte(keyInput), jwk.WithPEM(true))
		if err != nil {
			return nil, fmt.Errorf("failed to import private key: %w", err)
		}
	}

	if err = priv.Set(jwk.AlgorithmKey, alg); err != nil {
		return nil, fmt.Errorf("failed to set algorithm on private key: %w", err)
	}

	return &Keys{PrivateKey: priv}, nil
}

// ParsePrivateKeyFromFile reads a PEM-encoded RSA private key from the specified file,
// parses it, and returns a Keys struct containing the corresponding jwk.Key object.
func ParsePrivateKeyFromFile(keyFile string, algorithm string) (*Keys, error) {
	keyBytes, err := os.ReadFile(filepath.Clean(keyFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	return ParsePrivateKey(string(keyBytes), algorithm)
}

// ParsePublicKey takes a PEM-encoded RSA public key string, parses it,
// and returns a Keys struct containing the corresponding jwk.Key object.
func ParsePublicKey(keyInput, algorithm string) (*Keys, error) {
	if keyInput == "" {
		return nil, fmt.Errorf("no key input provided")
	}

	alg, err := jwa.KeyAlgorithmFrom(algorithm)
	if err != nil {
		return nil, fmt.Errorf("invalid algorithm specified: %w", err)
	}
	encAlg, ok := alg.(jwa.KeyEncryptionAlgorithm)
	if !ok {
		return nil, fmt.Errorf("algorithm is not a valid key encryption algorithm: %w", err)
	}
	if encAlg.IsSymmetric() {
		return nil, fmt.Errorf("algorithm is not an asymmetric key encryption algorithm: %w", err)
	}

	pubPem, _ := pem.Decode([]byte(keyInput))
	if pubPem == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing the key")
	}
	parsedKey, err := x509.ParsePKIXPublicKey(pubPem.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKIX public key: %w", err)
	}
	publicKey, ok := parsedKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("unable to parse RSA public key: %w", err)
	}
	pub, err := jwk.Import(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to import public key: %w", err)
	}

	if err = pub.Set(jwk.AlgorithmKey, alg); err != nil {
		return nil, fmt.Errorf("failed to set algorithm on public key: %w", err)
	}

	return &Keys{PublicKey: pub}, nil
}

// ParsePublicKeyFromFile reads a PEM-encoded RSA public key from the specified file,
// parses it, and returns a Keys struct containing the corresponding jwk.Key object.
func ParsePublicKeyFromFile(keyFile string, algorithm string) (*Keys, error) {
	keyBytes, err := os.ReadFile(filepath.Clean(keyFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	return ParsePublicKey(string(keyBytes), algorithm)
}

// Encrypt takes a plaintext payload, encrypts it using JWE with the public key, and returns the encrypted result.
func (k *Keys) Encrypt(payload []byte) ([]byte, error) {
	var encryptionKey jwk.Key
	if k.PublicKey == nil && k.PrivateKey != nil {
		if symmetric, ok := k.PrivateKey.(jwk.SymmetricKey); ok {
			encryptionKey = symmetric
		}
	} else {
		encryptionKey = k.PublicKey
	}

	if encryptionKey == nil {
		return nil, fmt.Errorf("no public or symmetric key available for encryption")
	}
	alg, ok := encryptionKey.Algorithm()
	if !ok {
		return nil, fmt.Errorf("algorithm not specified")
	}

	encrypted, err := jwe.Encrypt(payload, jwe.WithKey(alg, encryptionKey))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt payload: %w", err)
	}
	return encrypted, nil
}

// Decrypt takes an encrypted JWE payload, decrypts it using the private key, and returns the decrypted result.
func (k *Keys) Decrypt(encrypted []byte) ([]byte, error) {
	m, err := jwe.Parse(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to parse payload: %w", err)
	}

	alg, ok := m.ProtectedHeaders().Algorithm()
	if !ok {
		return nil, fmt.Errorf("algorithm not specified in JWE headers")
	}

	decrypted, err := jwe.Decrypt(encrypted, jwe.WithKey(alg, k.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt payload: %w", err)
	}
	return decrypted, nil
}
