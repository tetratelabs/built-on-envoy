// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package jwe

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

type KeyInput struct {
	PrivateKey jwk.Key
	PublicKey  jwk.Key
}

func ParseKeys(priv string, pub string) (*KeyInput, error) {
	keys := &KeyInput{}
	if priv != "" {
		privKey, err := ParsePrivateKey(priv)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		keys.PrivateKey = privKey.PrivateKey
	}
	if pub != "" {
		pubKey, err := ParsePublicKey(pub)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %w", err)
		}
		keys.PublicKey = pubKey.PublicKey
	}
	return keys, nil
}

func ParseKeysFromFile(privFile string, pubFile string) (*KeyInput, error) {
	keys := &KeyInput{}
	if privFile != "" {
		privKey, err := ParsePrivateKeyFromFile(privFile)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key from file: %w", err)
		}
		keys.PrivateKey = privKey.PrivateKey
	}
	if pubFile != "" {
		pubKey, err := ParsePublicKeyFromFile(pubFile)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key from file: %w", err)
		}
		keys.PublicKey = pubKey.PublicKey
	}
	return keys, nil
}

func ParsePrivateKey(keyInput string) (*KeyInput, error) {
	if keyInput == "" {
		return nil, fmt.Errorf("no key input provided")
	}
	privPem, _ := pem.Decode([]byte(keyInput))
	if privPem == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing the key")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(privPem.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}
	privateKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("Unable to parse RSA private key: %w", err)
	}

	priv, err := jwk.Import(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to import private key: %w", err)
	}
	return &KeyInput{PrivateKey: priv}, nil
}

func ParsePrivateKeyFromFile(keyFile string) (*KeyInput, error) {
	keyBytes, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	return ParsePrivateKey(string(keyBytes))
}

func ParsePublicKey(keyInput string) (*KeyInput, error) {
	if keyInput == "" {
		return nil, fmt.Errorf("no key input provided")
	}
	pubPem, _ := pem.Decode([]byte(keyInput))
	if pubPem == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing the key")
	}
	parsedKey, err := x509.ParsePKIXPublicKey(pubPem.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}
	publicKey, ok := parsedKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("Unable to parse RSA public key: %w", err)
	}

	priv, err := jwk.Import(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to import public key: %w", err)
	}
	return &KeyInput{PublicKey: priv}, nil
}

func ParsePublicKeyFromFile(keyFile string) (*KeyInput, error) {
	keyBytes, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	return ParsePublicKey(string(keyBytes))
}

func (k *KeyInput) Encrypt(payload []byte) ([]byte, error) {
	encrypted, err := jwe.Encrypt([]byte(payload), jwe.WithKey(jwa.RSA_OAEP(), k.PublicKey))
	if err != nil {
		fmt.Printf("failed to encrypt payload: %s\n", err)
		return nil, err
	}
	return encrypted, nil
}

func (k *KeyInput) Decrypt(encrypted []byte) ([]byte, error) {
	decrypted, err := jwe.Decrypt(encrypted, jwe.WithKey(jwa.RSA_OAEP(), k.PrivateKey))
	if err != nil {
		fmt.Printf("failed to decrypt payload: %s\n", err)
		return nil, err
	}
	return decrypted, nil
}
