// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package jwe parses configured private keys and decrypts payloads for the JWE filter.
package jwe

import (
	"fmt"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// Keys holds the private key used for JWE decryption.
type Keys struct {
	PrivateKey jwk.Key
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
