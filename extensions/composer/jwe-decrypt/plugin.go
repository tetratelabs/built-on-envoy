// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the JWE decryption filter.
package impl

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	boeJwe "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt/jwe"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const defaultMetadataNamespace = "io.builtonenvoy.jwe-decrypt"

// Config represents the JSON configuration for this filter.
type jweDecryptConfig struct {
	// PrivateKey is the PKCS8 private key used for decryption, provided either via a file path or inline.
	// When using inline, the value must be the base64-encoded PEM content.
	PrivateKey pkg.DataSource `json:"private_key"`
	// Algorithm is the JWE algorithm to use for decryption.
	Algorithm string `json:"algorithm"`
	// InputHeader is the name of the header that contains the JWE string to be decrypted. Defaults to Authorization if not specified.
	InputHeader string `json:"input_header"`
	// Prefix is an optional prefix to remove from the input header value before decryption (e.g., "Bearer ").
	Prefix string `json:"prefix"`
	// OutputHeader is the name of the header where the decrypted payload will be stored.
	OutputHeader string `json:"output_header"`
	// OutputMetadata specifies the metadata namespace and key under which the decrypted payload will be stored.
	// The namespace defaults to "io.builtonenvoy.jwe-decrypt" if not specified.
	OutputMetadata *pkg.MetadataKey `json:"output_metadata"`

	privateKey *boeJwe.Keys
}

func (f *jweDecryptConfig) getKey() (*boeJwe.Keys, error) {
	content, err := f.PrivateKey.Content()
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}
	if f.PrivateKey.Inline != "" {
		// Inline keys are base64-encoded PEM; decode before parsing.
		content, err = base64.StdEncoding.DecodeString(string(content))
		if err != nil {
			return nil, fmt.Errorf("failed to base64 decode inline private key: %w", err)
		}
	}
	return boeJwe.ParsePrivateKey(string(content), f.Algorithm)
}

// This is the implementation of the HTTP filter.
type jweDecryptHttpFilter struct { //nolint:revive
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *jweDecryptConfig
}

func (f *jweDecryptHttpFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	jweHeaderValues := headers.Get(f.config.InputHeader)
	if len(jweHeaderValues) == 0 {
		f.handle.Log(shared.LogLevelInfo, "jwe-decrypt: no JWE found in header "+f.config.InputHeader)
		return shared.HeadersStatusContinue
	}

	for _, jweValueBuffer := range jweHeaderValues {
		jweValue := jweValueBuffer.ToUnsafeString()
		f.handle.Log(shared.LogLevelInfo, "Decrypting: "+jweValue)

		// Handle prefix if specified
		encrypted := jweValue
		if f.config.Prefix != "" && len(jweValue) > len(f.config.Prefix) && jweValue[:len(f.config.Prefix)] == f.config.Prefix {
			encrypted = jweValue[len(f.config.Prefix):]
		}

		payload, err := f.config.privateKey.Decrypt([]byte(encrypted))
		if err != nil {
			f.handle.Log(shared.LogLevelError, "jwe-decrypt: failed to decrypt JWE: "+err.Error())
			continue
		}

		// Put prefix back if it was removed
		if f.config.Prefix != "" {
			payload = append([]byte(f.config.Prefix), payload...)
		}

		if f.config.OutputHeader != "" {
			f.handle.RequestHeaders().Set(f.config.OutputHeader, string(payload))
		}
		if f.config.OutputMetadata != nil {
			f.handle.SetMetadata(f.config.OutputMetadata.Namespace, f.config.OutputMetadata.Key, payload)
		}
	}

	return shared.HeadersStatusContinue
}

// This is the factory for the HTTP filter.
type jweDecryptHttpFilterFactory struct { //nolint:revive
	shared.EmptyHttpFilterFactory
	config *jweDecryptConfig
}

func (f *jweDecryptHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &jweDecryptHttpFilter{handle: handle, config: f.config}
}

// JWEDecryptHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type JWEDecryptHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON configuration and creates a factory for the HTTP filter.
func (f *JWEDecryptHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "jwe-decrypt: empty config")
		return nil, fmt.Errorf("empty config")
	}

	cfg := jweDecryptConfig{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "jwe-decrypt: failed to parse config: "+err.Error())
		return nil, err
	}

	if err := cfg.PrivateKey.Validate(); err != nil {
		handle.Log(shared.LogLevelError, "jwe-decrypt: invalid key config: "+err.Error())
		return nil, err
	}

	if cfg.Algorithm == "" {
		handle.Log(shared.LogLevelError, "jwe-decrypt: missing algorithm in key config")
		return nil, fmt.Errorf("missing algorithm in key config")
	}

	// Parse private key from config (either from file or inline)
	k, err := cfg.getKey()
	if err != nil {
		handle.Log(shared.LogLevelError, "jwe-decrypt: failed to get decryption key set: "+err.Error())
		return nil, err
	}
	cfg.privateKey = k

	// Default input header to "Authorization" if not specified
	if cfg.InputHeader == "" {
		cfg.InputHeader = "Authorization"
	}
	// Default metadata namespace if not specified
	if cfg.OutputMetadata != nil {
		if cfg.OutputMetadata.Namespace == "" {
			cfg.OutputMetadata.Namespace = defaultMetadataNamespace
		}
		if err := cfg.OutputMetadata.Validate(); err != nil {
			handle.Log(shared.LogLevelError, "jwe-decrypt: invalid output metadata config: "+err.Error())
			return nil, err
		}
	}

	return &jweDecryptHttpFilterFactory{config: &cfg}, nil
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		"jwe-decrypt": &JWEDecryptHttpFilterConfigFactory{},
	}
}
